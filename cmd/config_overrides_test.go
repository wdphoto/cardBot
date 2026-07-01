package cmd

import (
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

func testOverrideViperAndFlags() (*viper.Viper, *pflag.FlagSet) {
	v := newCommandViper()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("dest", "", "")
	flags.Bool("dry-run", false, "")
	flags.Bool("setup", false, "")
	flags.Bool("daemon", false, "")
	bindRootFlags(v, flags)
	return v, flags
}
