package pool

import (
	"context"
	"errors"
	"strings"

	"github.com/AtomiCloud/nitroso-tin/lib/encryptor"
	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/otelredis"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	"github.com/rs/zerolog"
)

// Pool maintains a multi-account KTMB userData pool in Redis, keyed by account
// email. It is deliberately separate from lib/session (the reserver/enricher
// single-session loginer) and never touches the ktmb:userData key.
//
// Storage is a Redis HASH (p.key): field = email, value = encrypted userData.
type Pool struct {
	ktmb      *ktmb.Ktmb
	main      *otelredis.OtelRedis
	logger    *zerolog.Logger
	key       string
	encryptor encryptor.Encryptor[string]
	creds     []config.Credential
}

func New(k *ktmb.Ktmb, main *otelredis.OtelRedis, logger *zerolog.Logger, key string,
	enc encryptor.Encryptor[string], creds []config.Credential) Pool {
	return Pool{
		ktmb:      k,
		main:      main,
		logger:    logger,
		key:       key,
		encryptor: enc,
		creds:     creds,
	}
}

// Sync reconciles the pool hash to match the configured accounts. It is
// idempotent: re-running with the same config performs no logins and no writes.
//
//   - add    (configured, not in pool) -> Login + HSET email -> encrypted userData
//   - remove (in pool, not configured) -> HDEL email
//   - keep   (in both)                 -> left untouched (no re-login)
//
// Partial-failure tolerant: a failed login on an add is logged and skipped; the
// remove set is config-driven and independent of login outcomes.
func (p *Pool) Sync(ctx context.Context) error {
	l := p.logger.With().Ctx(ctx).Str("poolKey", p.key).Logger()

	// desired set of emails from config
	desired := make(map[string]config.Credential, len(p.creds))
	for _, c := range p.creds {
		email := strings.TrimSpace(c.Email)
		if email == "" {
			l.Warn().Msg("Skipping credential with empty email")
			continue
		}
		desired[email] = c
	}

	// current set of emails already in the pool
	currentKeys, err := p.main.HKeys(ctx, p.key).Result()
	if err != nil {
		l.Error().Err(err).Msg("Failed to read current pool members")
		return err
	}
	current := make(map[string]struct{}, len(currentKeys))
	for _, e := range currentKeys {
		current[e] = struct{}{}
	}

	l.Info().
		Int("desired", len(desired)).
		Int("current", len(current)).
		Msg("Reconciling userData pool")

	// net-remove: in pool but no longer configured
	for email := range current {
		if _, ok := desired[email]; ok {
			continue
		}
		if er := p.main.HDel(ctx, p.key, email).Err(); er != nil {
			l.Error().Err(er).Str("email", email).Msg("Failed to remove account from pool")
			return er
		}
		l.Info().Str("email", email).Msg("Removed account from pool")
	}

	// net-add: configured but not in pool. Keep (already present) is skipped.
	added := 0
	for email, cred := range desired {
		if _, ok := current[email]; ok {
			l.Debug().Str("email", email).Msg("Account already in pool, leaving untouched")
			continue
		}

		l.Info().Str("email", email).Msg("Logging in new account")
		login, er := p.ktmb.Login(cred.Email, cred.Password)
		if er != nil {
			l.Error().Err(er).Str("email", email).Msg("Failed to login, skipping account")
			continue
		}
		if !login.Status {
			l.Error().Strs("errors", login.Messages).Str("email", email).Msg("Login rejected, skipping account")
			continue
		}

		enc, ee := p.encryptor.Encrypt(login.Data.UserData)
		if ee != nil {
			l.Error().Err(ee).Str("email", email).Msg("Failed to encrypt userData, skipping account")
			continue
		}

		if se := p.main.HSet(ctx, p.key, email, enc).Err(); se != nil {
			l.Error().Err(se).Str("email", email).Msg("Failed to store userData in pool")
			return se
		}
		added++
		l.Info().Str("email", email).Msg("Added account to pool")
	}

	l.Info().Int("added", added).Msg("Pool reconcile complete")
	return nil
}

// Pick returns one decrypted userData chosen at random from the pool (via
// HRANDFIELD). Errors if the pool is empty. Intended for consumers (e.g. the
// poller) that need a single random session token per job.
func (p *Pool) Pick(ctx context.Context) (string, error) {
	vals, err := p.main.HRandFieldWithValues(ctx, p.key, 1).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to pick from userData pool")
		return "", err
	}
	if len(vals) == 0 {
		return "", errors.New("userData pool is empty")
	}
	return p.encryptor.Decrypt(vals[0].Value)
}

// Get reads and decrypts the entire pool, returning a map of email -> userData.
// Intended for the future helium consumer / tests.
func (p *Pool) Get(ctx context.Context) (map[string]string, error) {
	raw, err := p.main.HGetAll(ctx, p.key).Result()
	if err != nil {
		p.logger.Error().Ctx(ctx).Err(err).Msg("Failed to read userData pool")
		return nil, err
	}

	out := make(map[string]string, len(raw))
	var errs []string
	for email, enc := range raw {
		ud, de := p.encryptor.Decrypt(enc)
		if de != nil {
			p.logger.Error().Ctx(ctx).Err(de).Str("email", email).Msg("Failed to decrypt userData")
			errs = append(errs, email)
			continue
		}
		out[email] = ud
	}
	if len(errs) > 0 {
		return out, errors.New("failed to decrypt userData for: " + strings.Join(errs, ", "))
	}
	return out, nil
}
