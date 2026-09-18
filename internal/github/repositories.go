package github

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type repositoryResponse struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (value repositoryResponse) repository() (Repository, error) {
	if value.ID <= 0 || !pathSegment(value.Owner.Login) || !pathSegment(value.Name) || value.FullName != value.Owner.Login+"/"+value.Name {
		return Repository{}, &Error{Kind: "unavailable"}
	}
	return Repository{ID: value.ID, Owner: value.Owner.Login, Name: value.Name, FullName: value.FullName}, nil
}

func (c *Client) Repositories(ctx context.Context, token string, page int) (RepositoryPage, error) {
	if !validCredential(token) || page <= 0 {
		return RepositoryPage{}, &Error{Kind: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	query := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}}
	payload, header, _, err := c.request(ctx, http.MethodGet, "https://api.github.com/user/repos?"+query.Encode(), token, nil)
	if err != nil {
		return RepositoryPage{}, err
	}
	var response []repositoryResponse
	if err := decode(payload, &response); err != nil {
		return RepositoryPage{}, err
	}
	if response == nil || len(response) > 100 {
		return RepositoryPage{}, &Error{Kind: "unavailable"}
	}
	next, err := nextPage(header.Values("Link"), page)
	if err != nil {
		return RepositoryPage{}, err
	}
	result := RepositoryPage{Items: make([]Repository, 0, len(response)), NextPage: next}
	for _, value := range response {
		repo, err := value.repository()
		if err != nil {
			return RepositoryPage{}, err
		}
		result.Items = append(result.Items, repo)
	}
	return result, nil
}

func (c *Client) Repository(ctx context.Context, token, owner, name string) (Repository, error) {
	if !validCredential(token) || !pathSegment(owner) || !pathSegment(name) {
		return Repository{}, &Error{Kind: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	target := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	payload, _, _, err := c.request(ctx, http.MethodGet, target, token, nil)
	if err != nil {
		return Repository{}, err
	}
	var response repositoryResponse
	if err := decode(payload, &response); err != nil {
		return Repository{}, err
	}
	repo, err := response.repository()
	if err != nil {
		return Repository{}, err
	}
	if !strings.EqualFold(repo.Owner, owner) || !strings.EqualFold(repo.Name, name) {
		return Repository{}, &Error{Kind: "unavailable"}
	}
	return repo, nil
}

func nextPage(headers []string, current int) (int, error) {
	next := 0
	for _, header := range headers {
		for _, link := range strings.Split(header, ",") {
			parts := strings.Split(link, ";")
			isNext := false
			for _, parameter := range parts[1:] {
				key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
				if found && key == "rel" {
					for _, relation := range strings.Fields(strings.Trim(value, "\"")) {
						if relation == "next" {
							isNext = true
						}
					}
				}
			}
			if !isNext {
				continue
			}
			raw := strings.TrimSpace(parts[0])
			if !strings.HasPrefix(raw, "<") || !strings.HasSuffix(raw, ">") {
				return 0, &Error{Kind: "unavailable"}
			}
			parsed, err := url.Parse(raw[1 : len(raw)-1])
			if err != nil || parsed.Scheme != "https" || parsed.Host != "api.github.com" || parsed.User != nil || parsed.Path != "/user/repos" || parsed.RawPath != "" || parsed.Fragment != "" {
				return 0, &Error{Kind: "unavailable"}
			}
			query, err := url.ParseQuery(parsed.RawQuery)
			if err != nil || len(query["page"]) != 1 || len(query["per_page"]) != 1 || query.Get("per_page") != "100" || len(query) != 2 {
				return 0, &Error{Kind: "unavailable"}
			}
			page, err := strconv.Atoi(query.Get("page"))
			if err != nil || page <= current || next != 0 {
				return 0, &Error{Kind: "unavailable"}
			}
			next = page
		}
	}
	return next, nil
}
