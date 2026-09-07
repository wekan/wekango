## About Money: No bounty

There is no bounty, like described at [CONTRIBUTING.md](CONTRIBUTING.md),
because wekango is NOT Big Tech. wekango is part of WeKan. wekango and WeKan is FLOSS.

## Steps for Coordinated Vulnerability Disclosure

### 1. Security Researcher:

- **Report**: Click "New draft security advisory" at [wekango Security Advisories](https://github.com/wekan/wekango/security/advisories).
  More info at [GitHub Docs about reporting privately](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-privately).
- **Proof of Concept (PoC)**: If possible, include a fix or a reproduction script/code.
- DO NOT EMAIL [security@wekan.fi](security-at-wekan.fi.asc), because:
  - Email is NOT secure or private.
  - Using GitHub directly verifies that:
    - You are:
      - Saving my time by filling all details at GitHub, so that I can easily click request CVE at GitHub
      - A correct GitHub user
      - Really reading this SECURITY.md
      - Knowing what you are doing
    - You are not:
      - Someone that tries to waste my time
      - Trying to impersonate as some other GitHub user
      - Spammer
      - Bot

### 2. wekango Security Team:

- **Remediation**: Please wait for a new wekango release that addresses the issue.
  Fixes are announced at the top of the [ChangeLog](https://github.com/wekan/wekango/blob/main/CHANGELOG.md).
- **Recognition**: We will acknowledge your contribution by adding you to our
  [Hall of Fame](https://wekan.fi/hall-of-fame/).
- **CVE Policy**: wekango Security requests CVE at GitHub when releasing Security Advisory.

### 3. Post-Release and Public Disclosure:

- Advisories reported at GitHub are listed at [wekango Security Advisories](https://github.com/wekan/wekango/security/advisories).
- DO NOT EMAIL [security@wekan.fi](security-at-wekan.fi.asc), because:
  - Email is NOT secure or private.
  - Using GitHub directly verifies that:
    - You are:
      - Saving my time by filling all details at GitHub, so that I can easily click request CVE at GitHub
      - A correct GitHub user
      - Really reading this SECURITY.md
      - Knowing what you are doing
    - You are not:
      - Someone that tries to waste my time
      - Trying to impersonate as some other GitHub user
      - Spammer
      - Bot

## Who can participate in the program

Anyone who reports a unique security issue in scope and does not disclose it to
a third party before we have patched, and is able to add
"New draft security advisory" at [wekango Security Advisories](https://github.com/wekan/wekango/security/advisories).

## Which domains are in scope?

No public domains, because all those are donated to WeKan Open Source project that develops wekango,
and we don't have any permissions to do security scans on those donated servers.

Please don't perform research that could impact other users. Second, please keep
the reports short and succinct. If we fail to understand the logic of your bug, we will tell you.

You can [Install wekango](https://github.com/wekan/wekango/releases) on your own computer
and scan it's vulnerabilities there.

## What wekango bugs are eligible?

Any typical web security bugs. If any of the previously mentioned is somehow problematic and
a security issue, we'd like to know about it, and also how to fix it:

- Cross-site Scripting
- Open redirect
- Cross-site request forgery
- File inclusion
- Authentication bypass
- Server-side code execution

## What wekango bugs are NOT eligible?

Typical already-known or 'no impact' bugs such as:

- Social engineering
- Denial of service
- SSL BEAST/CRIME/etc. WeKan does not have SSL built-in; it uses Caddy/Nginx/Apache on the front end.

wekango is Open Source with MIT license, and free to use also for commercial use.
