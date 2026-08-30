# Third-party notices

## UnityDoorstop

KeeperLoader uses UnityDoorstop as an external native bootstrap.

- Project: https://github.com/NeighTools/UnityDoorstop
- License: GNU Lesser General Public License v2.1
- Distribution: the executable-free Graveyard Keeper runtime package contains the official unmodified x64 proxy; the optional Manager can fetch the same official archive
- Integrity: release builds pin and verify the official UnityDoorstop 4.5.0 archive SHA-256 `7bb953e8d883c8bde76ced96f6d0e45660ad6e0151880d8ab5856bf4f532b147`
- Modifications: none

UnityDoorstop is a bootstrap utility and is not part of KeeperLoader's managed API or runtime implementation.

The compiled manager also includes the Go runtime, `golang.org/x/sys`, `github.com/lxn/walk`, `github.com/lxn/win`, and `gopkg.in/Knetic/govaluate.v3`. Their notices are reproduced in `THIRD_PARTY_LICENSES.txt`.
