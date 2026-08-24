// Package config loads, saves, and layers anexia configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
	goyaml "gopkg.in/yaml.v3"
)

// Keys lists every recognized configuration key, in display order.
var Keys = []string{"token", "api_base_url"}

// Config holds the persisted and resolved settings for anexia.
type Config struct {
	Token      string `koanf:"token"        yaml:"token,omitempty"`
	APIBaseURL string `koanf:"api_base_url" yaml:"api_base_url,omitempty"`
}

func unknownKeyError(key string) error {
	return fmt.Errorf("unknown config key %q: valid keys are %s", key, strings.Join(Keys, ", "))
}

// Path resolves the configuration file location. An explicit path wins,
// then $ANEXIA_CONFIG, then $XDG_CONFIG_HOME, then ~/.config.
func Path(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	if p := os.Getenv("ANEXIA_CONFIG"); p != "" {
		return p, nil
	}

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "anexia", "config.yaml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}

	return filepath.Join(home, ".config", "anexia", "config.yaml"), nil
}

// Load reads the configuration file. A missing file yields a zero Config and
// no error; a malformed file or an unrecognized key is an error.
func Load(explicit string) (Config, error) {
	path, err := Path(explicit)
	if err != nil {
		return Config{}, err
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	} else if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if err := validateKeys(k, path); err != nil {
		return Config{}, err
	}

	// The path is the one koanf just read through the same resolution, so this
	// opens no file the caller had not already asked for.
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path resolved by Path, not caller-supplied
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Decoded straight into the string fields rather than through koanf,
	// whose parser resolves an unquoted scalar by YAML's type rules first.
	// That turns a token of nothing but digits into a number and back into a
	// different string, which fails authentication while looking correct in
	// "config view". koanf is still used above, for the unknown-key check.
	var document goyaml.Node
	if err := goyaml.Unmarshal(raw, &document); err != nil {
		return Config{}, fmt.Errorf("decoding config %s: %w", path, err)
	}
	if len(document.Content) > 0 && document.Content[0].Tag == "!!null" {
		return Config{}, fmt.Errorf("decoding config %s: config document is null", path)
	}
	for _, key := range Keys {
		if value, found := mappingValue(&document, key, map[*goyaml.Node]bool{}); found && value.Tag == "!!null" {
			return Config{}, fmt.Errorf("decoding config %s: config key %q is null; quote it to use a null-like string",
				path, key)
		}
	}

	var cfg Config
	if err := goyaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("decoding config %s: %w", path, err)
	}

	return cfg, nil
}

// mappingValue resolves one effective YAML mapping value, following merge
// aliases while giving an explicit key precedence over a merged default.
func mappingValue(node *goyaml.Node, key string, seen map[*goyaml.Node]bool) (*goyaml.Node, bool) {
	if node == nil || seen[node] {
		return nil, false
	}
	seen[node] = true

	if node.Kind == goyaml.DocumentNode && len(node.Content) > 0 {
		return mappingValue(node.Content[0], key, seen)
	}
	if node.Alias != nil {
		return mappingValue(node.Alias, key, seen)
	}
	if node.Kind != goyaml.MappingNode {
		return nil, false
	}

	// Explicit values override anything inherited through <<.
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return resolveAlias(node.Content[i+1]), true
		}
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value != "<<" {
			continue
		}

		merged := node.Content[i+1]
		if merged.Kind == goyaml.SequenceNode {
			for _, child := range merged.Content {
				if value, found := mappingValue(child, key, seen); found {
					return value, true
				}
			}

			continue
		}

		return mappingValue(merged, key, seen)
	}

	return nil, false
}

func resolveAlias(node *goyaml.Node) *goyaml.Node {
	for node != nil && node.Alias != nil {
		node = node.Alias
	}

	return node
}

// validateKeys rejects keys koanf would otherwise silently ignore.
func validateKeys(k *koanf.Koanf, path string) error {
	known := make(map[string]struct{}, len(Keys))
	for _, key := range Keys {
		known[key] = struct{}{}
	}

	for key := range k.All() {
		top := strings.SplitN(key, ".", 2)[0]
		if _, ok := known[top]; !ok {
			return fmt.Errorf("reading config %s: %w", path, unknownKeyError(top))
		}
	}

	return nil
}

// Save writes cfg atomically, creating the parent directory if needed.
// The directory is mode 0700 and the file mode 0600.
func Save(explicit string, cfg Config) error {
	path, err := Path(explicit)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	data, err := goyaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temporary config file in %s: %w", dir, err)
	}

	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("setting permissions on %s: %w", tmpName, err)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("writing %s: %w", tmpName, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}

	return nil
}

// Set assigns value to the named configuration key.
func (c *Config) Set(key, value string) error {
	switch key {
	case "token":
		c.Token = value
	case "api_base_url":
		c.APIBaseURL = value
	default:
		return unknownKeyError(key)
	}

	return nil
}

// Get returns the value of the named key. The token is always masked.
func (c Config) Get(key string) (string, error) {
	switch key {
	case "token":
		return Mask(c.Token), nil
	case "api_base_url":
		return c.APIBaseURL, nil
	default:
		return "", unknownKeyError(key)
	}
}

// Redacted returns a copy of c with the token masked.
func (c Config) Redacted() Config {
	c.Token = Mask(c.Token)

	return c
}

// Mask hides all but the last four characters of a secret.
func Mask(s string) string {
	const visible = 4

	// Counted in runes, not bytes: slicing a multi-byte value by byte splits a
	// rune and emits a lone continuation byte, so the masked value would not
	// be text.
	r := []rune(s)

	switch {
	case len(r) == 0:
		return ""
	case len(r) <= visible:
		return strings.Repeat("*", visible)
	default:
		return strings.Repeat("*", len(r)-visible) + string(r[len(r)-visible:])
	}
}

// envToKey maps a supported environment variable to its config key. Anything
// else, and any empty value, maps to the empty key, which the provider drops.
func envToKey(name, value string) (key string, parsed any) {
	if value == "" {
		return "", nil
	}

	switch name {
	case "ANEXIA_TOKEN":
		return "token", value
	case "ANEXIA_API_BASE_URL":
		return "api_base_url", value
	default:
		return "", nil
	}
}

// flagToKey maps a command-line flag name to its config key.
var flagToKey = map[string]string{
	"token":        "token",
	"api-base-url": "api_base_url",
}

// Resolve layers the config file, then the environment, then any flags the
// user explicitly changed, and returns the effective configuration. A nil
// flags argument means no flag layer; a nil environ defaults to os.Environ.
func Resolve(explicit string, flags *pflag.FlagSet, environ func() []string) (Config, error) {
	fileCfg, err := Load(explicit)
	if err != nil {
		return Config{}, err
	}

	if environ == nil {
		environ = os.Environ
	}

	k := koanf.New(".")

	if err := k.Load(mapProvider{values: fileCfg.values()}, nil); err != nil {
		return Config{}, fmt.Errorf("layering config file values: %w", err)
	}

	envProvider := env.Provider(".", env.Opt{TransformFunc: envToKey, EnvironFunc: environ})
	if err := k.Load(envProvider, nil); err != nil {
		return Config{}, fmt.Errorf("layering environment values: %w", err)
	}

	if flags != nil {
		if err := k.Load(posflag.ProviderWithFlag(flags, ".", k, flagValue(flags)), nil); err != nil {
			return Config{}, fmt.Errorf("layering flag values: %w", err)
		}
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("decoding resolved config: %w", err)
	}

	return cfg, nil
}

// flagValue renames known flags to config keys and drops everything else.
// ProviderWithFlag only invokes the callback for flags the user changed, so
// unset defaults can never clobber file or environment values.
func flagValue(flags *pflag.FlagSet) func(*pflag.Flag) (string, any) {
	return func(f *pflag.Flag) (string, any) {
		key, ok := flagToKey[f.Name]
		if !ok {
			return "", nil
		}

		return key, posflag.FlagVal(flags, f)
	}
}

// values returns the non-empty fields of c keyed by config key.
func (c Config) values() map[string]any {
	values := map[string]any{}
	if c.Token != "" {
		values["token"] = c.Token
	}

	if c.APIBaseURL != "" {
		values["api_base_url"] = c.APIBaseURL
	}

	return values
}

// mapProvider adapts a plain map to the koanf Provider interface.
type mapProvider struct {
	values map[string]any
}

func (m mapProvider) ReadBytes() ([]byte, error) {
	return nil, errors.New("mapProvider does not support ReadBytes")
}

func (m mapProvider) Read() (map[string]any, error) {
	return m.values, nil
}
