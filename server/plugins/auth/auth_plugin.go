// Package auth loads the optional authentication plugin from the filesystem.
// The plugin is a Go source file interpreted at runtime by Yaegi.
// If the plugin file is absent the loader returns nil with no error, and the
// caller falls back to the built-in bcrypt authentication.
package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// pluginFile is the expected filename inside the authentication sub-directory.
const pluginFile = "authenticate.go"

// AuthPlugin wraps a scripted Authenticate function.
type AuthPlugin struct {
	authenticate func(user, password string) bool
	path         string
}

// Authenticate calls the scripted function and returns its result.
func (p *AuthPlugin) Authenticate(user, password string) bool {
	return p.authenticate(user, password)
}

// Path returns the filesystem path of the loaded plugin file.
func (p *AuthPlugin) Path() string { return p.path }

// Load looks for <pluginDir>/authentication/authenticate.go.
// Returns (nil, nil) when the file does not exist so callers can fall back
// to built-in authentication transparently.
func Load(pluginDir string) (*AuthPlugin, error) {
	scriptPath := filepath.Join(pluginDir, "authentication", pluginFile)

	src, err := os.ReadFile(scriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // plugin absent - not an error
		}
		return nil, fmt.Errorf("reading auth plugin %q: %w", scriptPath, err)
	}

	i := interp.New(interp.Options{})
	i.Use(stdlib.Symbols)

	if _, err := i.Eval(string(src)); err != nil {
		return nil, fmt.Errorf("evaluating auth plugin %q: %w", scriptPath, err)
	}

	// Resolve the exported Authenticate symbol from the package declared in the script.
	// The script must declare "package authenticate" at the top.
	v, err := i.Eval("authenticate.Authenticate")
	if err != nil {
		return nil, fmt.Errorf("auth plugin %q: cannot resolve Authenticate: %w", scriptPath, err)
	}

	fn, ok := v.Interface().(func(string, string) bool)
	if !ok {
		return nil, fmt.Errorf(
			"auth plugin %q: Authenticate has wrong type %s, want func(string,string)bool",
			scriptPath, reflect.TypeOf(v.Interface()),
		)
	}

	return &AuthPlugin{authenticate: fn, path: scriptPath}, nil
}
