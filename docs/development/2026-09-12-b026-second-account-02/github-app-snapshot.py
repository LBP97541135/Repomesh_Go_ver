#!/usr/bin/env python3
import base64
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.request

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import padding


AUTH_CONFIG = Path('/home/xubohan/.config/repomesh/auth.json')
API_ROOT = 'https://api.github.com'
API_VERSION = '2026-03-10'
ACCEPT = 'application/vnd.github+json'
MAX_RESPONSE_BYTES = 4 * 1024 * 1024


class SnapshotFailure(Exception):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


def fail(code):
    raise SnapshotFailure(code)


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def positive_integer(value, code):
    if isinstance(value, bool):
        fail(code)
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        fail(code)
    if parsed <= 0 or str(parsed) != str(value):
        fail(code)
    return parsed


def safe_login(value, code):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9](?:[A-Za-z0-9-]{0,98}[A-Za-z0-9])?', value):
        fail(code)
    return value


def safe_slug(value, code):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9](?:[A-Za-z0-9-]{0,98}[A-Za-z0-9])?', value):
        fail(code)
    return value


def json_object(value, code):
    if not isinstance(value, dict):
        fail(code)
    return value


def b64url(value):
    return base64.urlsafe_b64encode(value).rstrip(b'=')


def load_credentials():
    try:
        config = json.loads(AUTH_CONFIG.read_text(encoding='utf-8'))
        config = json_object(config, 'AUTH_CONFIG_INVALID')
        app_id = positive_integer(config.get('appId'), 'AUTH_CONFIG_INVALID')
        key_path = Path(config['privateKeyFile'])
        if not key_path.is_absolute() or key_path.is_symlink():
            fail('PRIVATE_KEY_INVALID')
        key_bytes = key_path.read_bytes()
        private_key = serialization.load_pem_private_key(key_bytes, password=None)
    except SnapshotFailure:
        raise
    except Exception:
        fail('AUTH_CONFIG_INVALID')
    if not hasattr(private_key, 'sign'):
        fail('PRIVATE_KEY_INVALID')
    return app_id, private_key


def app_jwt(app_id, private_key):
    now = int(time.time())
    header = b64url(json.dumps({'alg': 'RS256', 'typ': 'JWT'}, separators=(',', ':')).encode())
    payload = b64url(json.dumps({'iat': now - 60, 'exp': now + 540, 'iss': str(app_id)}, separators=(',', ':')).encode())
    signing_input = header + b'.' + payload
    try:
        signature = private_key.sign(signing_input, padding.PKCS1v15(), hashes.SHA256())
    except Exception:
        fail('JWT_SIGNING_FAILED')
    return (signing_input + b'.' + b64url(signature)).decode('ascii')


class GitHubClient:
    def __init__(self, opener=None):
        self.opener = opener or urllib.request.build_opener()

    def request(self, method, path, bearer, body=None):
        if not path.startswith('/') or '://' in path:
            fail('INTERNAL_PATH_INVALID')
        headers = {
            'Accept': ACCEPT,
            'Authorization': f'Bearer {bearer}',
            'X-GitHub-Api-Version': API_VERSION,
            'User-Agent': 'RepoMesh-B02.6-read-only-snapshot',
        }
        data = None
        if body is not None:
            headers['Content-Type'] = 'application/json'
            data = json.dumps(body, separators=(',', ':')).encode()
        request = urllib.request.Request(f'{API_ROOT}{path}', data=data, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=30) as response:
                raw = response.read(MAX_RESPONSE_BYTES + 1)
                if len(raw) > MAX_RESPONSE_BYTES:
                    fail('GITHUB_RESPONSE_TOO_LARGE')
                if response.status == 204:
                    return None
                return json.loads(raw)
        except SnapshotFailure:
            raise
        except urllib.error.HTTPError as error:
            if error.code in {401, 403, 404, 409, 422, 429}:
                fail(f'GITHUB_HTTP_{error.code}')
            fail('GITHUB_HTTP_ERROR')
        except (urllib.error.URLError, TimeoutError):
            fail('GITHUB_NETWORK_ERROR')
        except (ValueError, UnicodeDecodeError):
            fail('GITHUB_RESPONSE_INVALID')


def safe_account(value, code):
    source = json_object(value, code)
    return {
        'id': positive_integer(source.get('id'), code),
        'login': safe_login(source.get('login'), code),
    }


def safe_permissions(value):
    source = json_object(value, 'INSTALLATION_PERMISSIONS_INVALID')
    result = {}
    for name, level in source.items():
        if not isinstance(name, str) or not re.fullmatch(r'[a-z][a-z0-9_]{0,63}', name):
            fail('INSTALLATION_PERMISSIONS_INVALID')
        if level not in {'read', 'write', 'admin', 'none'}:
            fail('INSTALLATION_PERMISSIONS_INVALID')
        result[name] = level.upper()
    return dict(sorted(result.items()))


def app_projection(value, expected_app_id, expected_slug):
    source = json_object(value, 'APP_RESPONSE_INVALID')
    app_id = positive_integer(source.get('id'), 'APP_RESPONSE_INVALID')
    slug = safe_slug(source.get('slug'), 'APP_RESPONSE_INVALID')
    if app_id != expected_app_id or slug != expected_slug:
        fail('APP_IDENTITY_MISMATCH')
    result = {'id': app_id, 'slug': slug, 'owner': safe_account(source.get('owner'), 'APP_OWNER_INVALID')}
    if isinstance(source.get('public'), bool):
        result['public'] = source['public']
    return result


def installation_projection(value, expected_installation_id, expected_app_id):
    source = json_object(value, 'INSTALLATION_RESPONSE_INVALID')
    installation_id = positive_integer(source.get('id'), 'INSTALLATION_RESPONSE_INVALID')
    app_id = positive_integer(source.get('app_id'), 'INSTALLATION_RESPONSE_INVALID')
    if installation_id != expected_installation_id or app_id != expected_app_id:
        fail('INSTALLATION_IDENTITY_MISMATCH')
    selection = source.get('repository_selection')
    if selection not in {'all', 'selected'}:
        fail('INSTALLATION_SELECTION_INVALID')
    return {
        'id': installation_id,
        'account': safe_account(source.get('account'), 'INSTALLATION_ACCOUNT_INVALID'),
        'repositorySelection': selection.upper(),
        'suspended': source.get('suspended_at') is not None,
        'permissions': safe_permissions(source.get('permissions')),
    }


def create_installation_token(client, jwt, installation_id):
    value = json_object(
        client.request('POST', f'/app/installations/{installation_id}/access_tokens', jwt, {}),
        'INSTALLATION_TOKEN_RESPONSE_INVALID',
    )
    token = value.get('token')
    if not isinstance(token, str) or not token:
        fail('INSTALLATION_TOKEN_RESPONSE_INVALID')
    return token


def repositories_projection(client, token):
    repository_ids = []
    page_counts = []
    reported_total = None
    for page in range(1, 101):
        value = json_object(
            client.request('GET', f'/installation/repositories?per_page=100&page={page}', token),
            'REPOSITORIES_RESPONSE_INVALID',
        )
        repositories = value.get('repositories')
        if not isinstance(repositories, list) or len(repositories) > 100:
            fail('REPOSITORIES_RESPONSE_INVALID')
        total = positive_integer(value.get('total_count'), 'REPOSITORIES_RESPONSE_INVALID') if value.get('total_count') != 0 else 0
        if reported_total is None:
            reported_total = total
        elif reported_total != total:
            fail('REPOSITORIES_TOTAL_CHANGED')
        ids = [positive_integer(json_object(item, 'REPOSITORY_INVALID').get('id'), 'REPOSITORY_INVALID') for item in repositories]
        repository_ids.extend(ids)
        page_counts.append(len(ids))
        if len(ids) < 100:
            break
    else:
        fail('REPOSITORY_PAGE_LIMIT_EXCEEDED')
    if len(repository_ids) != len(set(repository_ids)) or len(repository_ids) != reported_total:
        fail('REPOSITORY_COUNT_MISMATCH')
    return {
        'repositoryIds': repository_ids,
        'reportedTotalCount': reported_total,
        'observedCount': len(repository_ids),
        'pageCount': len(page_counts),
        'pageItemCounts': page_counts,
    }


def snapshot(expected_slug, installation_id, client=None):
    app_id, private_key = load_credentials()
    jwt = app_jwt(app_id, private_key)
    github = client or GitHubClient()
    app = app_projection(github.request('GET', '/app', jwt), app_id, expected_slug)
    installation = installation_projection(
        github.request('GET', f'/app/installations/{installation_id}', jwt),
        installation_id,
        app_id,
    )
    token = None
    revoke_status = 'NOT_CREATED'
    try:
        token = create_installation_token(github, jwt, installation_id)
        repositories = repositories_projection(github, token)
        revoke_status = 'PENDING'
    finally:
        if token is not None:
            try:
                github.request('DELETE', '/installation/token', token)
                revoke_status = 'REVOKED'
            except SnapshotFailure:
                revoke_status = 'FAILED'
    return {
        'status': 'OK',
        'sampledAt': utc_now(),
        'apiVersion': API_VERSION,
        'app': app,
        'installation': installation,
        'repositories': repositories,
        'revokeStatus': revoke_status,
    }


def parse_arguments():
    if len(sys.argv) != 3:
        fail('INVALID_ARGUMENTS')
    slug = safe_slug(sys.argv[1], 'INVALID_APP_SLUG')
    installation_id = positive_integer(sys.argv[2], 'INVALID_INSTALLATION_ID')
    return slug, installation_id


def main():
    try:
        slug, installation_id = parse_arguments()
        result = snapshot(slug, installation_id)
        exit_code = 0
    except SnapshotFailure as error:
        result = {'status': 'FAIL', 'errorCode': error.code}
        exit_code = 1
    except Exception:
        result = {'status': 'FAIL', 'errorCode': 'SNAPSHOT_FAILED'}
        exit_code = 1
    sys.stdout.write(json.dumps(result, separators=(',', ':'), sort_keys=True) + '\n')
    return exit_code


if __name__ == '__main__':
    raise SystemExit(main())
