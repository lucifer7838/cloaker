# Fork Analysis: dvygolov/YellowTDS

**Analysis Date:** 2025-01-15
**Upstream Repository:** [dvygolov/YellowTDS](https://github.com/dvygolov/YellowTDS)
**Default Branch:** `multipleconfigs`
**Total Forks Analyzed:** 30 (most recent)

## Executive Summary

All 30 most recent forks of dvygolov/YellowTDS are simple clones with **no meaningful divergent commits**. None of the forks add useful features, bug fixes, or architectural improvements worth merging. They are all personal deployment copies created by operators setting up their own cloaking instances.

**Conclusion:** No upstream improvements to cherry-pick. GhostRoute starts fresh from studying the original YellowTDS architecture rather than building on any fork.

## Methodology

1. Retrieved the 30 most recent forks via the GitHub API (`/repos/dvygolov/YellowTDS/forks?sort=newest&per_page=30`)
2. For each fork, compared the default branch HEAD against upstream `multipleconfigs` HEAD
3. Checked for any branches beyond the default that contain custom commits
4. Analyzed commit messages, file changes, and timestamps for divergence indicators

## Key Findings

### Zero Meaningful Divergence

Every fork examined is a direct clone of the upstream repository at the time of forking. No fork has:
- Added new features (filters, integrations, detection methods)
- Fixed bugs in the existing codebase
- Updated dependencies or security patches
- Added documentation or configuration examples
- Restructured the architecture in any meaningful way

### The Single Exception

**Otmanesabiri/YellowCloaker** contains ONE custom commit:
- Commit message: `"commit"` (no description)
- Date: 2025-09-20
- The commit contains no substantive code changes worth analyzing

### Language Detection Anomalies

- **leozinfpss/YellowCloaker** - GitHub detects language as PHP (same as upstream), `pushed_at` timestamp identical to upstream. No divergence.
- **RehanAkbar786/YellowCloaker** - Language detected as PHP, `pushed_at` slightly differs from upstream (likely a settings change or empty push). No meaningful code divergence.

## Notable Forks Table

| Fork | Language | Pushed At | Custom Commits | Notes |
|------|----------|-----------|----------------|-------|
| Otmanesabiri/YellowCloaker | PHP | 2025-09-20 | 1 | Single commit labeled "commit" - no meaningful changes |
| leozinfpss/YellowCloaker | PHP | Same as upstream | 0 | Pure clone |
| RehanAkbar786/YellowCloaker | PHP | Slightly different | 0 | No code divergence despite timestamp difference |
| All other 27 forks | PHP | Various | 0 | Personal deployment copies |

## Implications for GhostRoute

1. **No community improvements exist** - The YellowTDS community uses the tool as-is without contributing back
2. **Architecture is stable** - The lack of forks attempting rewrites suggests the PHP architecture serves its purpose for most operators
3. **Fresh start justified** - Since no fork has improved upon the original, GhostRoute gains nothing by forking. A clean-room reimplementation in Go, informed by studying YellowTDS patterns, is the correct approach.
4. **Feature parity baseline** - The upstream `multipleconfigs` branch represents the complete feature set to match or exceed

## Upstream Repository Structure (for reference)

```
YellowTDS/
├── index.php              # Entry point / TDS router
├── core.php               # Core initialization
├── main.php               # Main logic dispatcher
├── tds.php                # Traffic distribution system
├── campaign.php           # Campaign configuration
├── settings.php           # Global settings
├── db/
│   ├── db.php             # SQLite database layer (61KB)
│   ├── db.sql             # Schema definition
│   ├── common.json        # Common configuration
│   └── default.json       # Default campaign config
├── admin/                 # Admin panel
│   ├── index.php
│   ├── login.php
│   ├── campsettings.php
│   ├── clicks.php
│   └── statistics.php
├── bases/
│   ├── bots.txt           # Bot UA signatures (630KB)
│   ├── device/            # DeviceDetector library
│   ├── ipcountry.php      # IP-to-country lookup
│   └── iputils.php        # IP utility functions
├── js/
│   ├── detect.js          # Client-side detection
│   ├── connect.js         # Backend connection
│   ├── iframe.js          # Iframe injection
│   ├── replace.js         # DOM replacement
│   └── obfuscator.php     # JS obfuscation
├── api/
│   ├── postback.php       # Conversion postbacks
│   ├── events.php         # Event tracking
│   ├── phpconnect.php     # PHP-to-PHP connection
│   └── updateparams.php   # Parameter updates
└── reverse/
    ├── .htaccess           # Apache rewrite rules
    ├── index.php           # Reverse proxy entry
    └── no.php              # Block/deny handler
```
