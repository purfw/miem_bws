package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type RefreshStore struct {
	rdb       *redis.Client
	ttl       time.Duration
	accessTTL time.Duration
}

func NewRefreshStore(rdb *redis.Client, ttl, accessTTL time.Duration) *RefreshStore {
	return &RefreshStore{rdb: rdb, ttl: ttl, accessTTL: accessTTL}
}

type refreshPayload struct {
	UserID      uuid.UUID `json:"user_id"`
	FamilyID    uuid.UUID `json:"family_id"`
	IssuedAt    time.Time `json:"issued_at"`
	UserAgent   string    `json:"user_agent,omitempty"`
	AuthVersion int64     `json:"auth_version"`
}

func tokenKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return "auth:refresh:" + hex.EncodeToString(h[:])
}

func usedTokenKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return "auth:refresh_used:" + hex.EncodeToString(h[:])
}

func userIndexKey(userID uuid.UUID) string       { return "auth:user_families:" + userID.String() }
func legacyUserIndexKey(userID uuid.UUID) string { return "auth:user_refresh:" + userID.String() }
func familyKey(familyID uuid.UUID) string        { return "auth:refresh_family:" + familyID.String() }

func (r *RefreshStore) Issue(ctx context.Context, userID uuid.UUID, ua string, versions ...int64) (string, error) {
	var version int64
	if len(versions) > 0 {
		version = versions[0]
	}
	return r.issue(ctx, userID, uuid.New(), ua, version)
}

func (r *RefreshStore) issue(ctx context.Context, userID, familyID uuid.UUID, ua string, version int64) (string, error) {
	tok := uuid.NewString()
	body, _ := json.Marshal(refreshPayload{UserID: userID, FamilyID: familyID, IssuedAt: time.Now(), UserAgent: ua, AuthVersion: version})
	pipe := r.rdb.TxPipeline()
	pipe.Set(ctx, tokenKey(tok), body, r.ttl)
	pipe.SAdd(ctx, familyKey(familyID), tokenKey(tok))
	pipe.Expire(ctx, familyKey(familyID), r.ttl)
	pipe.SAdd(ctx, userIndexKey(userID), familyID.String())
	pipe.Expire(ctx, userIndexKey(userID), r.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
	}
	return tok, nil
}

func (r *RefreshStore) lookup(ctx context.Context, key string) (refreshPayload, error) {
	v, err := r.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return refreshPayload{}, ErrRefreshNotFound
		}
		return refreshPayload{}, err
	}
	var p refreshPayload
	if err := json.Unmarshal(v, &p); err != nil {
		return refreshPayload{}, err
	}
	return p, nil
}

func (r *RefreshStore) Lookup(ctx context.Context, tok string) (uuid.UUID, error) {
	p, err := r.lookup(ctx, tokenKey(tok))
	return p.UserID, err
}

var rotateScript = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if not old then return 0 end
redis.call('DEL', KEYS[1])
redis.call('SREM', KEYS[3], KEYS[1])
redis.call('SET', KEYS[5], old, 'EX', ARGV[1])
redis.call('SET', KEYS[2], ARGV[2], 'EX', ARGV[1])
redis.call('SADD', KEYS[3], KEYS[2])
redis.call('EXPIRE', KEYS[3], ARGV[1])
redis.call('SADD', KEYS[4], ARGV[3])
redis.call('EXPIRE', KEYS[4], ARGV[1])
return 1
`)

// Rotate replaces a token atomically; reuse revokes the entire family.
func (r *RefreshStore) Rotate(ctx context.Context, oldTok, ua string) (string, uuid.UUID, error) {
	p, err := r.lookup(ctx, tokenKey(oldTok))
	if err != nil {
		if !errors.Is(err, ErrRefreshNotFound) {
			return "", uuid.Nil, err
		}
		used, usedErr := r.lookup(ctx, usedTokenKey(oldTok))
		if usedErr == nil {
			if err := r.RevokeFamily(ctx, used.UserID, used.FamilyID); err != nil {
				return "", uuid.Nil, err
			}
			return "", uuid.Nil, ErrRefreshReplay
		}
		return "", uuid.Nil, ErrRefreshNotFound
	}
	if p.FamilyID == uuid.Nil { // compatibility with tokens issued before families
		p.FamilyID = uuid.New()
	}
	newTok := uuid.NewString()
	newBody, _ := json.Marshal(refreshPayload{
		UserID: p.UserID, FamilyID: p.FamilyID, IssuedAt: time.Now(), UserAgent: ua, AuthVersion: p.AuthVersion,
	})
	result, err := rotateScript.Run(ctx, r.rdb, []string{
		tokenKey(oldTok), tokenKey(newTok), familyKey(p.FamilyID),
		userIndexKey(p.UserID), usedTokenKey(oldTok),
	}, int64(r.ttl.Seconds()), newBody, p.FamilyID.String()).Int()
	if err != nil {
		return "", uuid.Nil, err
	}
	if result != 1 {
		used, usedErr := r.lookup(ctx, usedTokenKey(oldTok))
		if usedErr == nil {
			if err := r.RevokeFamily(ctx, used.UserID, used.FamilyID); err != nil {
				return "", uuid.Nil, err
			}
			return "", uuid.Nil, ErrRefreshReplay
		}
		return "", uuid.Nil, ErrRefreshNotFound
	}
	return newTok, p.UserID, nil
}

func (r *RefreshStore) Revoke(ctx context.Context, tok string) error {
	p, err := r.lookup(ctx, tokenKey(tok))
	if err != nil {
		if errors.Is(err, ErrRefreshNotFound) {
			used, usedErr := r.lookup(ctx, usedTokenKey(tok))
			if usedErr == nil {
				return r.RevokeFamily(ctx, used.UserID, used.FamilyID)
			}
			if !errors.Is(usedErr, ErrRefreshNotFound) {
				return usedErr
			}
			return nil
		}
		return err
	}
	if p.FamilyID != uuid.Nil {
		return r.RevokeFamily(ctx, p.UserID, p.FamilyID)
	}
	return r.rdb.Del(ctx, tokenKey(tok)).Err()
}

var revokeFamilyScript = redis.NewScript(`
local members = redis.call('SMEMBERS', KEYS[1])
for _, member in ipairs(members) do
    if string.sub(member, 1, 13) == 'auth:refresh:' then redis.call('DEL', member) end
end
redis.call('DEL', KEYS[1])
redis.call('SREM', KEYS[2], ARGV[1])
return 1
`)

func (r *RefreshStore) RevokeFamily(ctx context.Context, userID, familyID uuid.UUID) error {
	if familyID == uuid.Nil {
		return r.RevokeAllForUser(ctx, userID)
	}
	return revokeFamilyScript.Run(ctx, r.rdb, []string{familyKey(familyID), userIndexKey(userID)}, familyID.String()).Err()
}

var revokeUserScript = redis.NewScript(`
local families = redis.call('SMEMBERS', KEYS[1])
for _, family in ipairs(families) do
    local key = 'auth:refresh_family:' .. family
    local members = redis.call('SMEMBERS', key)
    for _, member in ipairs(members) do
        if string.sub(member, 1, 13) == 'auth:refresh:' then redis.call('DEL', member) end
    end
    redis.call('DEL', key)
end
local legacy = redis.call('SMEMBERS', KEYS[2])
for _, member in ipairs(legacy) do
    if string.sub(member, 1, 13) == 'auth:refresh:' then redis.call('DEL', member) end
end
redis.call('DEL', KEYS[1], KEYS[2])
redis.call('SET', KEYS[3], ARGV[1], 'EX', ARGV[2])
return 1
`)

// The revocation marker must outlive every access token issued before it.
func (r *RefreshStore) revokedBeforeTTL() time.Duration {
	return r.ttl + r.accessTTL
}

func (r *RefreshStore) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	return revokeUserScript.Run(ctx, r.rdb, []string{userIndexKey(userID), legacyUserIndexKey(userID), "auth:revoked_before:" + userID.String()}, time.Now().Unix(), int64(r.revokedBeforeTTL().Seconds())).Err()
}

func (r *RefreshStore) BlockUser(ctx context.Context, userID uuid.UUID) error {
	if err := r.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}
	return r.rdb.Set(ctx, "auth:blocked:"+userID.String(), "1", 0).Err()
}

func (r *RefreshStore) UnblockUser(ctx context.Context, userID uuid.UUID) error {
	return r.rdb.Del(ctx, "auth:blocked:"+userID.String()).Err()
}

var (
	ErrRefreshNotFound = errors.New("refresh token not found")
	ErrRefreshReplay   = errors.New("refresh token replay detected")
)
