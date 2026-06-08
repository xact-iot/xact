package sqldb

import (
	"fmt"
	"os"
	"strings"
)

const (
	BootstrapAdminPasswordEnv     = "XACT_BOOTSTRAP_ADMIN_PASSWORD"
	BootstrapAdminPasswordFileEnv = "XACT_BOOTSTRAP_ADMIN_PASSWORD_FILE"
	UnsetBootstrapAdminHash       = "!xact-bootstrap-admin-password-unset!"
)

type AdminBootstrapCredential struct {
	Password string
	Source   string
	Set      bool
}

func ResolveBootstrapAdminPassword(defaultFile string) (AdminBootstrapCredential, error) {
	if password := strings.TrimSpace(os.Getenv(BootstrapAdminPasswordEnv)); password != "" {
		return AdminBootstrapCredential{
			Password: password,
			Source:   BootstrapAdminPasswordEnv,
			Set:      true,
		}, nil
	}

	file := strings.TrimSpace(os.Getenv(BootstrapAdminPasswordFileEnv))
	if file == "" {
		file = strings.TrimSpace(defaultFile)
	}
	if file != "" {
		if b, err := os.ReadFile(file); err == nil {
			if password := strings.TrimSpace(string(b)); password != "" {
				return AdminBootstrapCredential{
					Password: password,
					Source:   file,
					Set:      true,
				}, nil
			}
		} else if !os.IsNotExist(err) {
			return AdminBootstrapCredential{}, fmt.Errorf("reading bootstrap admin password file: %w", err)
		}
	}

	return AdminBootstrapCredential{Source: "first-login setup"}, nil
}

func IsBootstrapAdminPasswordUnset(passwordHash string) bool {
	return strings.TrimSpace(passwordHash) == UnsetBootstrapAdminHash
}
