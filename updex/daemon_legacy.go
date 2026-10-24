package updex

// The unit texts below are what earlier updex releases wrote into
// /etc/systemd/system for `updex daemon enable`, before the units carried an
// ownership marker (ADR-0007; the daily schedule). They are frozen literals,
// not generator output: the generator now writes different text, and these
// must keep matching what already sits on disk.
//
// DisableDaemon removes a unit file in /etc only when it carries the marker
// or matches one of these byte for byte. A file that differs by one byte (an
// administrator edited it, or an older release wrote another shape) is kept
// and reported, because deleting a file that updex cannot prove it wrote
// could remove an administrator's unit, or one the OS image ships in /etc.

// legacyTimerUnits is the ADR-0007 timer text.
var legacyTimerUnits = []string{`[Unit]
Description=Automatic sysext updates

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=3600s

[Install]
WantedBy=timers.target
`}

// legacyServiceUnits are the ADR-0007 service texts: the sandboxed unit
// (updex releases from #329 on) and the earlier unit without the sandbox
// block.
var legacyServiceUnits = []string{
	`[Unit]
Description=Automatic sysext update service

[Service]
Type=oneshot
ExecStart=/usr/bin/updex features update --no-refresh
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
RestrictNamespaces=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
SystemCallFilter=@system-service
`,
	`[Unit]
Description=Automatic sysext update service

[Service]
Type=oneshot
ExecStart=/usr/bin/updex features update --no-refresh
`,
}
