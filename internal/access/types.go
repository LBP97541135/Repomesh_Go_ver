package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

type Provider interface {
	AuthorizationURL(string, string) string
	Exchange(context.Context, string, string) (github.TokenSet, error)
	Refresh(context.Context, string) (github.TokenSet, error)
	Identity(context.Context, string) (github.Identity, error)
	Repositories(context.Context, string, int) (github.RepositoryPage, error)
	Repository(context.Context, string, string, string) (github.Repository, error)
	AppCapability(context.Context, string, string) (github.Capability, error)
	ObserveAppInstallation(context.Context, string, string) (github.AppInstallationObservation, error)
}

type Service struct {
	pool                             *pgxpool.Pool
	secrets                          *secrets.Store
	provider                         Provider
	projectDestinationResolver       ProjectDestinationResolver
	modelSaveDestinationResolver     ModelSaveDestinationResolver
	modelTestDestinationResolver     ModelTestDestinationResolver
	modelApplyDestinationResolver    ModelApplyDestinationResolver
	issueCreationDestinationResolver IssueCreationDestinationResolver
	appID                            string
	issueAppKeyVersion               secrets.VersionID
	// mode 是部署模式：public（默认）任何 GitHub 账号都能登录、每个账号各自
	// 一个空间、互相看不到对方的项目/仓库/中转站；private 只有该组织的成员
	// 能登录、全组织共享（登录闸属 P2，未实现）。空值按 public 处理——
	// 公有部署不需要改任何部署配置就是正确行为。
	mode string
}

func New(pool *pgxpool.Pool, store *secrets.Store, provider Provider) *Service {
	return &Service{pool: pool, secrets: store, provider: provider}
}

// SetDeploymentMode 设置部署模式。未知值一律按 public 处理：宁可放开，
// 也不因为一个拼错的配置把所有人锁在门外。
func (s *Service) SetDeploymentMode(mode string) {
	if mode == "private" {
		s.mode = "private"
		return
	}
	s.mode = "public"
}

type Failure struct {
	Status     int
	Code       string
	RetryAfter int
}

func (e *Failure) Error() string { return e.Code }

func failure(status int, code string) error { return &Failure{Status: status, Code: code} }
func unavailable() error                    { return failure(503, "RESULT_UNCONFIRMED") }

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	// IsAdmin 是该账号自己的授权事实，只用于前端隐藏控件；
	// 写路由每次请求另行校验（见 Service.IsAdmin），绝不拿它当权威。
	IsAdmin bool `json:"isAdmin"`
}

type ConnectionView struct {
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observedAt"`
}

type Session struct {
	User             User           `json:"user"`
	GitHubConnection ConnectionView `json:"githubConnection"`
	CSRFToken        string         `json:"csrfToken"`
	Binding          string         `json:"-"`
	Generation       int64          `json:"-"`
	GitHubID         int64          `json:"-"`
}

type StartResult struct {
	AttemptID        string    `json:"attemptId"`
	AuthorizationURL string    `json:"authorizationUrl"`
	ExpiresAt        time.Time `json:"expiresAt"`
	ResultPage       string    `json:"resultPage"`
}

type Receipt struct {
	CommittedRevision string `json:"committedRevision"`
	IsCurrent         bool   `json:"isCurrent"`
}

type AttemptResult struct {
	AttemptID  string     `json:"attemptId"`
	Purpose    string     `json:"purpose"`
	State      string     `json:"state"`
	ReasonCode *string    `json:"reasonCode"`
	ObservedAt *time.Time `json:"observedAt"`
	Connection *Receipt   `json:"connection"`
	NextPage   *string    `json:"nextPage"`
}

type StartCommand struct {
	ID            string
	Purpose       string
	BindingCookie string
	SessionCookie string
	CSRF          string
	Destination   Destination
}

type Started struct {
	Result        StartResult
	BindingCookie string
	Created       bool
}

type Callback struct{ BindingCookie, State, Code, ProviderError string }
type CallbackResult struct{ Page, SessionCookie string }

func randomToken() string {
	var data [32]byte
	rand.Read(data[:])
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func newID() string {
	var data [16]byte
	rand.Read(data[:])
	data[6] = data[6]&15 | 64
	data[8] = data[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:])
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validCookie(value string) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(data) == 32 && len(value) == 43
}

func ValidID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}

func providerUnauthorized(err error) bool {
	var upstream *github.Error
	return errors.As(err, &upstream) && upstream.Kind == "unauthorized"
}
