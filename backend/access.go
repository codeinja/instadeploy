package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

// Access protection: an optional password or 6-digit PIN that Pangolin
// asks for before anyone reaches a public service. It's meant for apps
// without a login of their own. Browsers get Pangolin's sign-in page; API
// clients and mobile apps can't pass it.
const (
	AccessNone     = "none"
	AccessPassword = "password"
	AccessPincode  = "pincode"
)

var pincodeRe = regexp.MustCompile(`^\d{6}$`)

// accessInput is how the dashboard sets protection for a service. An
// empty secret keeps the current one (when the mode doesn't change).
type accessInput struct {
	Service string `json:"service"`
	Mode    string `json:"mode"`
	Secret  string `json:"secret"`
}

func validateAccess(mode, secret string) error {
	switch mode {
	case AccessNone:
		return nil
	case AccessPassword:
		if len(secret) < 8 || len(secret) > 100 {
			return errors.New("the access password must be 8 to 100 characters")
		}
	case AccessPincode:
		if !pincodeRe.MatchString(secret) {
			return errors.New("the access PIN must be exactly 6 digits")
		}
	default:
		return errors.New("access must be none, password or pincode")
	}
	return nil
}

// setServiceAccess stores a service's protection. It returns false when
// the service doesn't exist.
func (s *Server) setServiceAccess(ctx context.Context, db dbtx, depID string, in accessInput) (bool, error) {
	var current string
	var currentSecret []byte
	err := db.QueryRowContext(ctx, `SELECT access, access_secret FROM services WHERE deployment_id = $1 AND name = $2`,
		depID, in.Service).Scan(&current, &currentSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if in.Mode != AccessNone && in.Mode != AccessPassword && in.Mode != AccessPincode {
		return true, errors.New("access must be none, password or pincode")
	}
	var encrypted []byte
	switch {
	case in.Mode == AccessNone:
	case in.Secret == "" && in.Mode == current && len(currentSecret) > 0:
		encrypted = currentSecret // keep the existing password or PIN
	default:
		if err := validateAccess(in.Mode, in.Secret); err != nil {
			return true, err
		}
		encrypted = s.box.Encrypt(in.Secret)
	}
	_, err = db.ExecContext(ctx, `UPDATE services SET access = $1, access_secret = $2, updated_at = now()
		WHERE deployment_id = $3 AND name = $4`, in.Mode, encrypted, depID, in.Service)
	return true, err
}

// accessFingerprint identifies what protection a route should have, so the
// reconciler can tell when Pangolin needs updating. "" means none.
func accessFingerprint(mode string, encryptedSecret []byte) string {
	if mode == AccessNone || mode == "" {
		return ""
	}
	sum := sha256.Sum256(append([]byte(mode+"\x00"), encryptedSecret...))
	return mode + ":" + hex.EncodeToString(sum[:8])
}

// syncRouteAccess applies each service's protection to its ready routes in
// Pangolin, where it differs from what was last applied.
func (s *Server) syncRouteAccess(ctx context.Context) error {
	if !s.pangolin.Enabled() {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.provider_ref, r.access_applied, sv.access, sv.access_secret, d.id, d.name, sv.name
		FROM routes r
		JOIN services sv ON sv.id = r.service_id
		JOIN deployments d ON d.id = r.deployment_id
		WHERE r.status = 'READY' AND r.provider_ref <> '' AND d.status <> 'DELETING'`)
	if err != nil {
		return err
	}
	type work struct {
		id, ref, applied, mode, depID, depName, service string
		secret                                          []byte
	}
	var list []work
	for rows.Next() {
		var w work
		if rows.Scan(&w.id, &w.ref, &w.applied, &w.mode, &w.secret, &w.depID, &w.depName, &w.service) == nil &&
			accessFingerprint(w.mode, w.secret) != w.applied {
			list = append(list, w)
		}
	}
	rows.Close()

	for _, w := range list {
		secret := ""
		if w.mode != AccessNone {
			plain, err := s.box.Decrypt(w.secret)
			if err != nil {
				s.logLine(ctx, w.depID, "", "deploy", w.service+": "+"Could not apply access protection: "+err.Error())
				continue
			}
			secret = plain
		}
		if err := s.pangolin.SetRouteAccess(ctx, w.ref, w.mode, secret); err != nil {
			s.logLine(ctx, w.depID, "", "deploy", w.service+": "+fmt.Sprintf("Could not apply access protection in Pangolin: %v", err))
			continue
		}
		s.db.ExecContext(ctx, `UPDATE routes SET access_applied = $1, updated_at = now() WHERE id = $2`,
			accessFingerprint(w.mode, w.secret), w.id)
		switch w.mode {
		case AccessPassword:
			s.logLine(ctx, w.depID, "", "deploy", w.service+": "+"Public URL protected with a password.")
		case AccessPincode:
			s.logLine(ctx, w.depID, "", "deploy", w.service+": "+"Public URL protected with a PIN.")
		default:
			s.logLine(ctx, w.depID, "", "deploy", w.service+": "+"Access protection removed from the public URL.")
		}
	}
	return nil
}
