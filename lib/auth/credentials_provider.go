package auth

import (
	"context"
	"errors"
	"fmt"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/descope/go-sdk/descope/client"
	"net/http"
	"time"
)

type CredentialsProvider interface {
	RequestEditor() func(ctx context.Context, req *http.Request) error
}

type DescopeM2MCredentialProvider struct {
	client     *client.DescopeClient
	accessKey  string
	token      *string
	expiration *int64
}

func NewDescopeM2MCredentialProvider(cfg config.DescopeConfig) (CredentialsProvider, error) {
	c, err := client.NewWithConfig(&client.Config{
		ProjectID:     cfg.DescopeId,
		ManagementKey: cfg.DescopeAccessKey,
	})
	if err != nil {
		return nil, err
	}
	cred := DescopeM2MCredentialProvider{
		client:    c,
		accessKey: cfg.DescopeAccessKey,
	}

	return &cred, nil
}

func (d *DescopeM2MCredentialProvider) getTokenRaw(c context.Context) (string, int64, error) {
	authenticated, token, err := d.client.Auth.ExchangeAccessKey(c, d.accessKey)

	if err != nil {
		return "", 0, err
	}

	if authenticated {
		return token.JWT, token.Expiration, nil
	}

	return "", 0, errors.New("failed to authenticate")
}

func (d *DescopeM2MCredentialProvider) getToken(c context.Context) (string, error) {

	if d.expiration != nil {

		exp := time.Unix(*d.expiration, 0)
		now := time.Now()
		isExpired := now.After(exp)

		if !isExpired {
			return *d.token, nil
		}

	}

	token, expiration, err := d.getTokenRaw(c)
	if err != nil {
		return "", err
	}
	d.token = &token
	d.expiration = &expiration
	return *d.token, nil
}

func (d *DescopeM2MCredentialProvider) RequestEditor() func(ctx context.Context, req *http.Request) error {
	return func(ctx context.Context, req *http.Request) error {
		token, err := d.getToken(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		return nil
	}
}
