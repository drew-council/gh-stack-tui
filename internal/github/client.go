package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/repository"
)

// Client talks to one repository on GitHub as the user logged in to the gh
// CLI. go-gh finds gh's token the way gh does: GH_TOKEN and friends, gh's
// config, or the system keyring.
type Client struct {
	Repo Repo
	gql  *api.GraphQLClient
	rest *api.RESTClient
}

// NewClient returns a client for repo, authenticated as the gh CLI's user.
func NewClient(repo Repo) (*Client, error) {
	host := repo.Host
	if host == "" {
		host, _ = auth.DefaultHost()
	}
	// Look the token up once: when it is in the keyring, each lookup runs
	// gh auth token.
	token, _ := auth.TokenForHost(host)
	if token == "" {
		return nil, fmt.Errorf("not logged in to %s: run gh auth login", host)
	}
	repo.Host = host
	return NewClientWith(repo, api.ClientOptions{Host: host, AuthToken: token})
}

// NewClientWith returns a client for repo with explicit go-gh options, for
// example a fake transport in tests.
func NewClientWith(repo Repo, opts api.ClientOptions) (*Client, error) {
	gql, err := api.NewGraphQLClient(opts)
	if err != nil {
		return nil, err
	}
	rest, err := api.NewRESTClient(opts)
	if err != nil {
		return nil, err
	}
	return &Client{Repo: repo, gql: gql, rest: rest}, nil
}

// CurrentRepo returns the GitHub repository that the working directory's git
// remotes point at, chosen the way gh chooses it.
func CurrentRepo() (Repo, error) {
	r, err := repository.Current()
	if err != nil {
		return Repo{}, err
	}
	return Repo{Host: r.Host, Owner: r.Owner, Name: r.Name}, nil
}

// graphQL runs query and decodes the response's data into out.
func (c *Client) graphQL(ctx context.Context, query string, vars map[string]any, out any) error {
	return c.gql.DoWithContext(ctx, query, vars, out)
}

// partial reports whether err is a GraphQL error that still came with data,
// such as one aliased PR number that no longer resolves.
func partial(err error) bool {
	var gqlErr *api.GraphQLError
	return errors.As(err, &gqlErr)
}

// RequestReviewers asks logins to review PR number.
func (c *Client) RequestReviewers(ctx context.Context, number int, logins []string) error {
	body, err := json.Marshal(map[string][]string{"reviewers": logins})
	if err != nil {
		return err
	}
	path := fmt.Sprintf(
		"repos/%s/%s/pulls/%d/requested_reviewers",
		c.Repo.Owner,
		c.Repo.Name,
		number,
	)
	return c.rest.DoWithContext(ctx, "POST", path, bytes.NewReader(body), nil)
}
