package systemd

import (
	"strings"
	"testing"
)

func TestGenerateTimer(t *testing.T) {
	tests := []struct {
		name     string
		config   *TimerConfig
		contains []string
		excludes []string
	}{
		{
			name: "minimal config",
			config: &TimerConfig{
				Name:        "test-timer",
				Description: "Test timer description",
				OnCalendar:  "daily",
			},
			contains: []string{
				"[Unit]",
				"Description=Test timer description",
				"[Timer]",
				"OnCalendar=daily",
				"[Install]",
				"WantedBy=timers.target",
			},
			excludes: []string{
				"Persistent=true",
				"RandomizedDelaySec=",
			},
		},
		{
			name: "with persistent",
			config: &TimerConfig{
				Name:        "persistent-timer",
				Description: "Persistent timer",
				OnCalendar:  "weekly",
				Persistent:  true,
			},
			contains: []string{
				"[Unit]",
				"Description=Persistent timer",
				"[Timer]",
				"OnCalendar=weekly",
				"Persistent=true",
				"[Install]",
				"WantedBy=timers.target",
			},
			excludes: []string{
				"RandomizedDelaySec=",
			},
		},
		{
			name: "with random delay",
			config: &TimerConfig{
				Name:           "delay-timer",
				Description:    "Timer with random delay",
				OnCalendar:     "*-*-* 04:00:00",
				RandomDelaySec: 3600,
			},
			contains: []string{
				"[Unit]",
				"Description=Timer with random delay",
				"[Timer]",
				"OnCalendar=*-*-* 04:00:00",
				"RandomizedDelaySec=3600s",
				"[Install]",
				"WantedBy=timers.target",
			},
			excludes: []string{
				"Persistent=true",
			},
		},
		{
			name: "full config",
			config: &TimerConfig{
				Name:           "updex-update",
				Description:    "Automatic sysext updates",
				OnCalendar:     "daily",
				Persistent:     true,
				RandomDelaySec: 1800,
			},
			contains: []string{
				"[Unit]",
				"Description=Automatic sysext updates",
				"[Timer]",
				"OnCalendar=daily",
				"Persistent=true",
				"RandomizedDelaySec=1800s",
				"[Install]",
				"WantedBy=timers.target",
			},
			excludes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateTimer(tt.config)

			// Check that all expected strings are present
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("GenerateTimer() output missing expected string %q\nGot:\n%s", expected, result)
				}
			}

			// Check that excluded strings are not present
			for _, excluded := range tt.excludes {
				if strings.Contains(result, excluded) {
					t.Errorf("GenerateTimer() output should not contain %q\nGot:\n%s", excluded, result)
				}
			}
		})
	}
}

func TestGenerateService(t *testing.T) {
	tests := []struct {
		name     string
		config   *ServiceConfig
		contains []string
	}{
		{
			name: "minimal config",
			config: &ServiceConfig{
				Name:        "test-service",
				Description: "Test service description",
				ExecStart:   "/usr/bin/test",
				Type:        "simple",
			},
			contains: []string{
				"[Unit]",
				"Description=Test service description",
				"[Service]",
				"Type=simple",
				"ExecStart=/usr/bin/test",
			},
		},
		{
			name: "oneshot type",
			config: &ServiceConfig{
				Name:        "updex-update",
				Description: "Automatic sysext update",
				ExecStart:   "/usr/bin/updex update --quiet",
				Type:        "oneshot",
			},
			contains: []string{
				"[Unit]",
				"Description=Automatic sysext update",
				"[Service]",
				"Type=oneshot",
				"ExecStart=/usr/bin/updex update --quiet",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateService(tt.config)

			// Check that all expected strings are present
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("GenerateService() output missing expected string %q\nGot:\n%s", expected, result)
				}
			}

			// Service should NOT have [Install] section (timer handles activation)
			if strings.Contains(result, "[Install]") {
				t.Errorf("GenerateService() should not contain [Install] section\nGot:\n%s", result)
			}
		})
	}
}

func TestGenerateTimerSectionOrder(t *testing.T) {
	// Verify sections appear in correct order: [Unit] -> [Timer] -> [Install]
	config := &TimerConfig{
		Name:        "order-test",
		Description: "Test section order",
		OnCalendar:  "daily",
		Persistent:  true,
	}

	result := GenerateTimer(config)

	unitIdx := strings.Index(result, "[Unit]")
	timerIdx := strings.Index(result, "[Timer]")
	installIdx := strings.Index(result, "[Install]")

	if unitIdx == -1 || timerIdx == -1 || installIdx == -1 {
		t.Fatalf("Missing required sections in output:\n%s", result)
	}

	if unitIdx >= timerIdx || timerIdx >= installIdx {
		t.Errorf("Sections not in correct order. Expected [Unit] < [Timer] < [Install]\nUnitIdx=%d, TimerIdx=%d, InstallIdx=%d\nGot:\n%s",
			unitIdx, timerIdx, installIdx, result)
	}
}

func TestGenerateServiceSectionOrder(t *testing.T) {
	// Verify sections appear in correct order: [Unit] -> [Service]
	config := &ServiceConfig{
		Name:        "order-test",
		Description: "Test section order",
		ExecStart:   "/usr/bin/test",
		Type:        "oneshot",
	}

	result := GenerateService(config)

	unitIdx := strings.Index(result, "[Unit]")
	serviceIdx := strings.Index(result, "[Service]")

	if unitIdx == -1 || serviceIdx == -1 {
		t.Fatalf("Missing required sections in output:\n%s", result)
	}

	if unitIdx >= serviceIdx {
		t.Errorf("Sections not in correct order. Expected [Unit] < [Service]\nUnitIdx=%d, ServiceIdx=%d\nGot:\n%s",
			unitIdx, serviceIdx, result)
	}
}

// TestGenerateServiceDaemonSandbox renders the service unit exactly as
// `updex daemon enable` does (Sandbox: true) and checks that every hardening
// directive the daemon unit relies on is present in the [Service] section.
func TestGenerateServiceDaemonSandbox(t *testing.T) {
	config := &ServiceConfig{
		Name:        "updex-update",
		Description: "Automatic sysext update service",
		ExecStart:   "/usr/bin/updex features update --no-refresh",
		Type:        "oneshot",
		Sandbox:     true,
	}

	result := GenerateService(config)

	directives := []string{
		"NoNewPrivileges=yes",
		"ProtectSystem=full",
		"ProtectHome=yes",
		"PrivateTmp=yes",
		"ProtectKernelTunables=yes",
		"ProtectKernelModules=yes",
		"ProtectKernelLogs=yes",
		"ProtectControlGroups=yes",
		"ProtectClock=yes",
		"ProtectHostname=yes",
		"RestrictRealtime=yes",
		"RestrictSUIDSGID=yes",
		"RestrictNamespaces=yes",
		"LockPersonality=yes",
		"MemoryDenyWriteExecute=yes",
		"SystemCallArchitectures=native",
		"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		"SystemCallFilter=@system-service",
	}
	for _, directive := range directives {
		if !strings.Contains(result, directive+"\n") {
			t.Errorf("GenerateService() sandboxed output missing directive %q\nGot:\n%s", directive, result)
		}
	}

	// The daemon unit keeps its ADR-0007 shape.
	for _, expected := range []string{"Type=oneshot", "ExecStart=/usr/bin/updex features update --no-refresh"} {
		if !strings.Contains(result, expected) {
			t.Errorf("GenerateService() sandboxed output missing %q\nGot:\n%s", expected, result)
		}
	}

	// Sandboxing directives belong to [Service], which is the last section.
	serviceIdx := strings.Index(result, "[Service]")
	if idx := strings.Index(result, "NoNewPrivileges=yes"); idx < serviceIdx {
		t.Errorf("sandbox directives must appear inside [Service]\nGot:\n%s", result)
	}
	if strings.Contains(result, "[Install]") {
		t.Errorf("GenerateService() should not contain [Install] section\nGot:\n%s", result)
	}
}

// TestGenerateServiceWithoutSandbox pins the default: callers that do not opt in
// get the same minimal unit as before, with no hardening directives.
func TestGenerateServiceWithoutSandbox(t *testing.T) {
	config := &ServiceConfig{
		Name:        "plain-service",
		Description: "Plain service",
		ExecStart:   "/usr/bin/test",
		Type:        "simple",
	}

	result := GenerateService(config)

	for _, directive := range []string{"NoNewPrivileges=", "ProtectSystem=", "SystemCallFilter="} {
		if strings.Contains(result, directive) {
			t.Errorf("GenerateService() without Sandbox should not contain %q\nGot:\n%s", directive, result)
		}
	}
}
