// Package systemd provides types and functions for generating and managing
// systemd unit files (timers and services) for scheduling automatic updates.
package systemd

import (
	"fmt"
	"strings"
)

// TimerConfig represents configuration for a systemd timer unit.
type TimerConfig struct {
	// Name is the unit name without extension (e.g., "updex-update")
	Name string
	// Description is the human-readable description for the [Unit] section
	Description string
	// OnCalendar is the timer schedule (e.g., "daily" or "*-*-* 04:00:00")
	OnCalendar string
	// Persistent runs the timer if it missed the last start time
	Persistent bool
	// RandomDelaySec randomizes the start time within this window (in seconds)
	RandomDelaySec int
}

// ServiceConfig represents configuration for a systemd service unit.
type ServiceConfig struct {
	// Name is the unit name without extension (e.g., "updex-update")
	Name string
	// Description is the human-readable description for the [Unit] section
	Description string
	// ExecStart is the full command to execute (e.g., "/usr/bin/updex update --quiet")
	ExecStart string
	// Type is the service type (e.g., "oneshot", "simple")
	Type string
	// Sandbox emits the systemd hardening directives in SandboxDirectives
	// into the [Service] section. The auto-update daemon unit sets it; other
	// callers keep the minimal unit unless they opt in.
	Sandbox bool
}

// SandboxDirectives are the systemd hardening directives emitted, in this
// order, into the [Service] section when ServiceConfig.Sandbox is set. They
// confine the root auto-update service without breaking the staged-update
// path: ProtectSystem=full (not strict) keeps /usr, /boot, /efi and /etc
// read-only while leaving /var writable, so the default
// /var/lib/extensions.d staging directory, the /var/lib/extensions link
// directory, and hand-written transfers with a Target.Path elsewhere under
// /var keep working. No CapabilityBoundingSet is set.
var SandboxDirectives = []string{
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

// GenerateTimer generates a systemd timer unit file content from the config.
// The returned string contains valid systemd unit file syntax with [Unit],
// [Timer], and [Install] sections.
func GenerateTimer(cfg *TimerConfig) string {
	var b strings.Builder

	// [Unit] section
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", cfg.Description)
	b.WriteString("\n")

	// [Timer] section
	b.WriteString("[Timer]\n")
	fmt.Fprintf(&b, "OnCalendar=%s\n", cfg.OnCalendar)
	if cfg.Persistent {
		b.WriteString("Persistent=true\n")
	}
	if cfg.RandomDelaySec > 0 {
		fmt.Fprintf(&b, "RandomizedDelaySec=%ds\n", cfg.RandomDelaySec)
	}
	b.WriteString("\n")

	// [Install] section
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")

	return b.String()
}

// GenerateService generates a systemd service unit file content from the config.
// The returned string contains valid systemd unit file syntax with [Unit] and
// [Service] sections. No [Install] section is generated since the timer
// handles activation. When cfg.Sandbox is set, SandboxDirectives are appended
// to the [Service] section after ExecStart.
func GenerateService(cfg *ServiceConfig) string {
	var b strings.Builder

	// [Unit] section
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", cfg.Description)
	b.WriteString("\n")

	// [Service] section
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "Type=%s\n", cfg.Type)
	fmt.Fprintf(&b, "ExecStart=%s\n", cfg.ExecStart)
	if cfg.Sandbox {
		for _, directive := range SandboxDirectives {
			b.WriteString(directive)
			b.WriteString("\n")
		}
	}

	return b.String()
}
