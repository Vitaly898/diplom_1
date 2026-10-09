package config

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tt := range []struct {
		name                               string
		env                                map[string]string
		args                               []string
		address, database, accrual, secret string
		wantErr                            bool
	}{
		{name: "missing database", wantErr: true},
		{name: "missing accrual", env: map[string]string{envDatabaseURI: "db"}, wantErr: true},
		{name: "environment", env: map[string]string{envDatabaseURI: "db", envAccrualAddress: "http://accrual", envRunAddress: ":9000", envTokenSecret: "secret"}, address: ":9000", database: "db", accrual: "http://accrual", secret: "secret"},
		{name: "default address", env: map[string]string{envDatabaseURI: "db", envAccrualAddress: "http://accrual"}, address: ":8080", database: "db", accrual: "http://accrual"},
		{name: "flags override environment", env: map[string]string{envDatabaseURI: "env-db", envAccrualAddress: "http://env", envRunAddress: ":9000"}, args: []string{"-a", ":7000", "-d", "flag-db", "-r", "http://flag"}, address: ":7000", database: "flag-db", accrual: "http://flag"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{envRunAddress, envDatabaseURI, envAccrualAddress, envTokenSecret} {
				t.Setenv(name, "")
			}
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			oldFlags, oldArgs := flag.CommandLine, os.Args
			flag.CommandLine = flag.NewFlagSet(tt.name, flag.ContinueOnError)
			os.Args = append([]string{"gophermart"}, tt.args...)
			t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if cfg.RunAddress != tt.address || cfg.DatabaseURI != tt.database || cfg.AccrualSystemAddress != tt.accrual {
				t.Fatalf("unexpected configuration: %+v", cfg)
			}
			if tt.secret != "" && cfg.TokenSecret != tt.secret {
				t.Fatal("environment token secret ignored")
			}
			if tt.secret == "" {
				data, err := base64.StdEncoding.DecodeString(cfg.TokenSecret)
				if err != nil || len(data) != 32 {
					t.Fatal("invalid generated secret")
				}
			}
		})
	}
}

func TestRequiredEnvironmentPresence(t *testing.T) {
	for _, key := range []string{envDatabaseURI, envAccrualAddress} {
		for _, present := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/present=%v", key, present), func(t *testing.T) {
				t.Setenv(envDatabaseURI, "db")
				t.Setenv(envAccrualAddress, "http://accrual")
				t.Setenv(key, "")
				if !present {
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
				oldFlags, oldArgs := flag.CommandLine, os.Args
				flag.CommandLine = flag.NewFlagSet("presence", flag.ContinueOnError)
				os.Args = []string{"gophermart"}
				defer func() { flag.CommandLine, os.Args = oldFlags, oldArgs }()
				_, err := Load()
				if err == nil || strings.Contains(err.Error(), "пустой строкой") != present {
					t.Fatalf("present=%v err=%v", present, err)
				}
			})
		}
	}
}
