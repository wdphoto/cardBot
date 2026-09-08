package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/wdphoto/cardBot/config"
)

func TestApplyConfigOverrides_PreservesFileConfigWithoutFlagOrEnv(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Destination.Path = "/from-config"
	v, flags := testOverrideViperAndFlags()

	applyConfigOverrides(cfg, v, flags)

	if cfg.Destination.Path != "/from-config" {
		t.Fatalf("Destination.Path = %q, want file config value", cfg.Destination.Path)
	}
}

func TestApplyConfigOverrides_FlagOverridesFileConfig(t *testing.T) {
	t.Parallel()

	cfg := config.Defaults()
	cfg.Destination.Path = "/from-config"
	v, flags := testOverrideViperAndFlags()
	if err := flags.Set("dest", "/from-flag"); err != nil {
		t.Fatal(err)
	}

	applyConfigOverrides(cfg, v, flags)

	if cfg.Destination.Path != "/from-flag" {
		t.Fatalf("Destination.Path = %q, want flag value", cfg.Destination.Path)
	}
}

func TestApplyConfigOverrides_EnvOverridesFileConfig(t *testing.T) {
	t.Setenv("CARDBOT_DESTINATION", "/from-env")

	cfg := config.Defaults()
	cfg.Destination.Path = "/from-config"
	v, flags := testOverrideViperAndFlags()

	applyConfigOverrides(cfg, v, flags)

	if cfg.Destination.Path != "/from-env" {
		t.Fatalf("Destination.Path = %q, want env value", cfg.Destination.Path)
	}
}

func TestApplyConfigOverrides_UnboundEnvKeysInert(t *testing.T) {
	// Clear the real override env vars so an inherited value cannot alter cfg.
	t.Setenv("CARDBOT_DESTINATION", "")
	t.Setenv("CARDBOT_NAMING", "")
	t.Setenv("CARDBOT_LOG_FILE", "")
	t.Setenv("CARDBOT_VERIFY_MODE", "")

	// applyConfigOverrides leaves the dry-run/setup/daemon AutomaticEnv values
	// unread, so they cannot activate behavior through this function.
	t.Setenv("CARDBOT_DAEMON", "1")
	t.Setenv("CARDBOT_SETUP", "1")
	t.Setenv("CARDBOT_DRY_RUN", "1")

	// The operational root flags are read directly from opts (not viper); their
	// getters must remain false under the env vars, without running RunE.
	root := NewRootCommand(BuildInfo{Version: "test"})
	for _, name := range []string{"dry-run", "setup", "daemon"} {
		if b, _ := root.Flags().GetBool(name); b {
			t.Errorf("root flag %q unexpectedly true under env", name)
		}
	}

	// applyConfigOverrides leaves the whole config unchanged (override env cleared).
	want := config.Defaults()
	cfg := config.Defaults()
	v, flags := testOverrideViperAndFlags()
	applyConfigOverrides(cfg, v, flags)
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("config changed unexpectedly:\n got %+v\nwant %+v", cfg, want)
	}
}

func testOverrideViperAndFlags() (*viper.Viper, *pflag.FlagSet) {
	v := newCommandViper()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	// Use StringVar backing storage (the production --dest pattern) so the
	// destination binding is exercised through the real flag.Value path.
	var dest string
	flags.StringVar(&dest, "dest", "", "")
	flags.Bool("dry-run", false, "")
	flags.Bool("setup", false, "")
	flags.Bool("daemon", false, "")
	bindRootFlags(v, flags)
	return v, flags
}
