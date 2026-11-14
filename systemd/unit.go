// Package systemd provides types and functions for generating and managing
// systemd unit files (timers and services) for scheduling automatic updates.
package systemd

import (
	"fmt"
	"strings"
)

// TimerConfig represents configuration for a systemd timer unit.
type TimerConfig struct {
	// Comment is an optional single comment line written first in the unit
	// file, as "# <Comment>". Empty writes no comment line.
	Comment string
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
	// Comment is an optional single comment line written first in the unit
	// file, as "# <Comment>". Empty writes no comment line.
	Comment string
	// Name is the unit name without extension (e.g., "updex-update")
	Name string
	// Description is the human-readable description for the [Unit] section
	Description string
	// Wants lists units written as one "Wants=" line in [Unit] (for example
	// "network-online.target"). Empty writes no line.
	Wants []string
	// After lists units written as one "After=" line in [Unit]. Empty writes
	// no line.
	After []string
	// Before lists units written as one "Before=" line in [Unit], after
	// After=. Empty writes no line.
	Before []string
	// Conditions are complete [Unit] directive lines such as
	// "ConditionACPower=true", written one per line after Before=.
	Conditions []string
	// UnitDirectives are further complete [Unit] directive lines, such as
	// "DefaultDependencies=no", written one per line after Conditions.
	UnitDirectives []string
	// ExecStart is the full command to execute (e.g., "/usr/bin/updex update --quiet")
	ExecStart string
	// Type is the service type (e.g., "oneshot", "simple")
	Type string
	// TimeoutStartSec bounds the start of the unit (for a oneshot, the whole
	// run), written as "TimeoutStartSec=<value>" after ExecStart. Empty
	// writes no line and keeps systemd's default.
	TimeoutStartSec string
	// Directives are complete [Service] directive lines, written one per
	// line after TimeoutStartSec= and before the Sandbox directives.
	Directives []string
	// Sandbox emits the systemd hardening directives in SandboxDirectives
	// into the [Service] section. The auto-update daemon unit sets it; other
	// callers keep the minimal unit unless they opt in.
	Sandbox bool
	// WantedBy lists the targets written as one "WantedBy=" line in an
	// [Install] section. Empty writes no [Install] section: a timer
	// activates the service.
	WantedBy []string
}

// RefreshSandboxDirectives are the hardening directives of a service that
// runs a systemd-sysext refresh (updex-suite-switch.service, ADR-0021). They
// are the SandboxDirectives that neither give the service its own mount
// namespace nor block what the refresh does. systemd-sysext mounts its
// overlays where the service runs, so a private mount namespace
// (ProtectSystem=, ProtectHome=, PrivateTmp=, ProtectKernelTunables=,
// ProtectControlGroups=, ProtectHostname=) would hide the merged images from
// the host; it unshares a mount namespace itself (RestrictNamespaces=); it
// attaches loop devices, which the device policy of ProtectClock= forbids, and
// waits for them over netlink (RestrictAddressFamilies=); it can load the
// overlay or loop module (ProtectKernelModules=); and the refresh sequence runs
// systemd-sysusers and systemd-tmpfiles, which write /etc and can create
// setgid directories (RestrictSUIDSGID=). The syscall allow-list of
// SystemCallFilter= is not proven for that sequence, so it is not set either.
// What stays restricts the process without a namespace.
var RefreshSandboxDirectives = []string{
	"NoNewPrivileges=yes",
	"RestrictRealtime=yes",
	"LockPersonality=yes",
	"MemoryDenyWriteExecute=yes",
	"SystemCallArchitectures=native",
}

// SandboxDirectives are the systemd hardening directives emitted, in this
// order, into the [Service] section when ServiceConfig.Sandbox is set. They
// confine the root auto-update service without breaking the staged-update
// path: ProtectSystem=full (not strict) keeps /usr, /boot, /efi and /etc
// read-only while leaving /var writable, so the default
// /var/lib/extensions.d staging directory, the /var/lib/extensions link
// directory, and hand-written transfers with a Target.Path elsewhere under
// /var keep working. No CapabilityBoundingSet is set.
//
// The syscall filter allows @mount in addition to @system-service, because
// the minimum image version check (ADR-0016) runs "bootc status --json", and
// bootc remounts /sysroot read-write with mount(8) for that command
// (frostyard/updex#463). Without @mount, seccomp kills that mount with SIGSYS
// and the check fails closed on every run. The remount stays in the service's
// own mount namespace: ProtectSystem= gives the service one, systemd does not
// propagate mounts from it to the host, and bootc then uses it instead of
// unsharing its own (which RestrictNamespaces=yes would refuse). @mount does
// not widen what a compromised updex can do on the host: it runs as root,
// can reach the systemd manager over AF_UNIX, and links sysext images that
// systemd-sysext merges into /usr. The sandbox contains bugs, not an
// attacker with code execution in updex.
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
	"SystemCallFilter=@system-service @mount",
}

// writeComment writes comment as the first line of a unit file when it is
// not empty.
func writeComment(b *strings.Builder, comment string) {
	if comment != "" {
		fmt.Fprintf(b, "# %s\n", comment)
	}
}

// GenerateTimer generates a systemd timer unit file content from the config.
// The returned string contains valid systemd unit file syntax with [Unit],
// [Timer], and [Install] sections.
func GenerateTimer(cfg *TimerConfig) string {
	var b strings.Builder
	writeComment(&b, cfg.Comment)

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
// [Service] sections. An [Install] section is generated only when WantedBy is
// set; without it a timer handles activation. When cfg.Sandbox is set,
// SandboxDirectives are appended to the [Service] section after ExecStart,
// TimeoutStartSec and Directives. Wants, After, Before, Conditions and
// UnitDirectives, when set, follow Description in [Unit]; a config that sets
// none of the newer fields (Before, UnitDirectives, Directives, WantedBy) and no
// Comment produces the same text as before they existed.
func GenerateService(cfg *ServiceConfig) string {
	var b strings.Builder
	writeComment(&b, cfg.Comment)

	// [Unit] section
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", cfg.Description)
	if len(cfg.Wants) > 0 {
		fmt.Fprintf(&b, "Wants=%s\n", strings.Join(cfg.Wants, " "))
	}
	if len(cfg.After) > 0 {
		fmt.Fprintf(&b, "After=%s\n", strings.Join(cfg.After, " "))
	}
	if len(cfg.Before) > 0 {
		fmt.Fprintf(&b, "Before=%s\n", strings.Join(cfg.Before, " "))
	}
	for _, condition := range cfg.Conditions {
		b.WriteString(condition)
		b.WriteString("\n")
	}
	for _, directive := range cfg.UnitDirectives {
		b.WriteString(directive)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// [Service] section
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "Type=%s\n", cfg.Type)
	fmt.Fprintf(&b, "ExecStart=%s\n", cfg.ExecStart)
	if cfg.TimeoutStartSec != "" {
		fmt.Fprintf(&b, "TimeoutStartSec=%s\n", cfg.TimeoutStartSec)
	}
	for _, directive := range cfg.Directives {
		b.WriteString(directive)
		b.WriteString("\n")
	}
	if cfg.Sandbox {
		for _, directive := range SandboxDirectives {
			b.WriteString(directive)
			b.WriteString("\n")
		}
	}

	if len(cfg.WantedBy) > 0 {
		b.WriteString("\n[Install]\n")
		fmt.Fprintf(&b, "WantedBy=%s\n", strings.Join(cfg.WantedBy, " "))
	}

	return b.String()
}
