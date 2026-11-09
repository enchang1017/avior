// Package stackenv resolves the connection strings the acceptance suite uses to
// reach one local stack.
//
// Several worktrees run the suite at once, each against a stack of its own. A
// suite that quietly reached the default stack because one variable was left
// unset would pass while testing somebody else's database, so resolution is
// all or nothing: every DSN is named, or none is and all of them derive from
// the stack's published Postgres port.
package stackenv

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultStack is the main checkout's stack, on the ports it has always had.
	DefaultStack = "avior"
	// DefaultPostgresPort is DefaultStack's published Postgres port.
	DefaultPostgresPort = 55432

	stackKey = "AVIOR_STACK"
	portKey  = "AVIOR_PG_PORT"
)

// DSNs are the six credentials the acceptance suite holds, one per role.
type DSNs struct {
	Store        string
	Target       string
	Write        string
	Deploy       string
	Provision    string
	FixtureOwner string
}

type credential struct {
	key      string
	userinfo string
	database string
	field    func(*DSNs) *string
}

var credentials = []credential{
	{"AVIOR_STORE_DSN", "avior:avior", "avior", func(d *DSNs) *string { return &d.Store }},
	{"AVIOR_TARGET_DSN", "avior_governed:avior_governed_local", "aviorfixture", func(d *DSNs) *string { return &d.Target }},
	{"AVIOR_WRITE_DSN", "avior_writer:avior_writer_local", "aviorfixture", func(d *DSNs) *string { return &d.Write }},
	{"AVIOR_DEPLOY_DSN", "avior_deployer:avior_deployer_local", "aviordeploy", func(d *DSNs) *string { return &d.Deploy }},
	{"AVIOR_PROVISION_DSN", "avior_provisioner:avior_provisioner_local", "postgres", func(d *DSNs) *string { return &d.Provision }},
	{"AVIOR_FIXTURE_OWNER_DSN", "avior:avior", "aviorfixture", func(d *DSNs) *string { return &d.FixtureOwner }},
}

// Keys are the variables Resolve reads a DSN from, in a stable order.
func Keys() []string {
	keys := make([]string, 0, len(credentials))
	for _, c := range credentials {
		keys = append(keys, c.key)
	}
	return keys
}

// Resolve reads the DSNs through getenv.
//
// All six set: they are used as given. None set: each derives from
// AVIOR_PG_PORT on localhost, which defaults to DefaultPostgresPort only for
// DefaultStack. Anything in between is an error naming what is missing.
func Resolve(getenv func(string) string) (DSNs, error) {
	var set, missing []string
	for _, c := range credentials {
		if strings.TrimSpace(getenv(c.key)) == "" {
			missing = append(missing, c.key)
		} else {
			set = append(set, c.key)
		}
	}

	switch {
	case len(missing) == 0:
		return fromEnv(getenv), nil
	case len(set) > 0:
		return DSNs{}, fmt.Errorf(
			"%s set but %s unset: name every acceptance DSN or none, "+
				"so a partial override cannot reach another stack",
			strings.Join(set, ", "), strings.Join(missing, ", "))
	}

	port, err := postgresPort(getenv)
	if err != nil {
		return DSNs{}, err
	}
	return derived(port), nil
}

func fromEnv(getenv func(string) string) DSNs {
	var d DSNs
	for _, c := range credentials {
		*c.field(&d) = strings.TrimSpace(getenv(c.key))
	}
	return d
}

func derived(port int) DSNs {
	var d DSNs
	for _, c := range credentials {
		*c.field(&d) = fmt.Sprintf("postgres://%s@localhost:%d/%s?sslmode=disable",
			c.userinfo, port, c.database)
	}
	return d
}

func postgresPort(getenv func(string) string) (int, error) {
	stack := strings.TrimSpace(getenv(stackKey))
	raw := strings.TrimSpace(getenv(portKey))
	if raw == "" {
		if stack != "" && stack != DefaultStack {
			return 0, fmt.Errorf(
				"%s=%s names an isolated stack but %s is unset; "+
					"run through make, which derives it", stackKey, stack, portKey)
		}
		return DefaultPostgresPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New(portKey + "=" + raw + " is not a TCP port")
	}
	return port, nil
}
