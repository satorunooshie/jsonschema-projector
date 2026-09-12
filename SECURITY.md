# Security Policy

## Supported versions

Only the latest release on the default branch is supported with security
fixes. Users should upgrade before reporting issues against an older release.

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting for this repository. Do not
open a public issue for an undisclosed vulnerability or include exploit details
in a pull request.

Include the affected version, reproduction steps, impact, and any suggested
mitigation. Reports will be handled privately until a fix or mitigation is
available.

## Scope

The CLI reads schemas and configuration supplied by its caller and writes
artifacts to configured paths. Reports involving path traversal, arbitrary file
access, denial of service from malicious schemas, or unsafe generated output
are in scope.
