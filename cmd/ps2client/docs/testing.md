# Testing against ps2link

Unit tests (`go test ./pkg/ps2client/...`) cover packet encoding, the file
server and the connection handling on loopback. The end-to-end tests in
`pkg/ps2client/e2e` run the client against a real ps2link. They are skipped
unless `PS2CLIENT_E2E_HOST` is set.

## PCSX2 on Windows

ps2link needs the PC to connect *in* to the PS2, which PCSX2's default Sockets
network mode cannot do, and the Npcap loopback adapter is rejected by PCSX2
(Npcap reports it as a loopback capture, not Ethernet). A TAP adapter works:

1. Install the TAP-Windows6 driver (github.com/OpenVPN/tap-windows6, release
   `dist.win10.zip`): from an elevated shell, run `devcon.exe install
   OemVista.inf tap0901` from the `amd64` folder.
2. Rename the new adapter, for example `PS2 TAP`, and give it `169.254.250.1`
   with mask `255.255.0.0`.
3. In PCSX2, set the Ethernet API to TAP and pick that adapter, or in
   `PCSX2.ini` set `EthApi = TAP` and `EthDevice = {adapter GUID}`.
4. Turn off the host filesystem (`HostFs = false` under `[EmuCore]`). While it is
   on, PCSX2 answers the `host:` device itself and ps2link's driver is never
   asked.

## ps2link build for the emulator

Two changes to a copy of ps2link, neither needed on hardware:

- In `iop/net_fsys.c` rename the device (`fsname`) from `host` to `pkfs`. PCSX2
  keeps the `host` name even with the host filesystem off, so programs then use
  `pkfs:` paths.
- In `include/globals.h` set `DEFAULT_IP`, `DEFAULT_NETMASK` and `DEFAULT_GW` to
  `169.254.250.2`, `255.255.0.0` and `169.254.250.1`. Without a host filesystem
  ps2link cannot read `IPCONFIG.DAT`.

The network stack must be an `ps2ip-nm` that registers with NetMan and reads its
address from its start-up arguments. A build of it that only contains the plain
stack has no network interface, so ps2link comes up but never transmits.

## Running

Build `pkg/ps2client/e2e/hosttest` with `defines: ['DEV=\"pkfs:\"']`, boot
the emulator with the ps2link build above, then:

    PS2CLIENT_E2E_HOST=169.254.250.2 \
    PS2CLIENT_E2E_HOSTTEST=path/to/hosttest.elf \
    PS2CLIENT_E2E_DEVICE=pkfs: \
    go test -tags e2e -v -count=1 -timeout 600s ./pkg/ps2client/e2e

Each test resets ps2link first. On hardware leave `PS2CLIENT_E2E_DEVICE` unset
and use an unmodified ps2link.

## Known limits of the emulator run

- `dumpreg fifo` aborts PCSX2 with a VIF FIFO assertion, so the register test
  skips that group.
- Console text from a plain `printf` does not reach udptty in PCSX2: the
  emulator handles writes to stdout itself, before any driver sees them. Text
  written to `tty0:` explicitly does, and the e2e suite checks that it arrives.
- `execiop`, `gsexec`, `scrdump`, `netdump`, `startvu` and `stopvu` were checked
  by hand against ps2link's own log; they have no PC-visible result to assert.

## Real hardware

Use a ps2link built with the fixed `ps2ip-nm` (sdk-core PR "build the NetMan
variant"); earlier release binaries cannot transmit. Put `PS2LINK.ELF` and an
`IPCONFIG.DAT` (`ip mask gateway` on one line) in one folder, start it, then from
the PC:

    ps2client -h <ps2-ip> listen
    ps2client -h <ps2-ip> -t 30 execee host:consoletest.elf
    ps2client -h <ps2-ip> -v -t 30 execee host:hosttest.elf

`examples/consoletest` prints with plain `printf`. On hardware stdout goes to the
`tty0:` device that udptty broadcasts on UDP port 18194, so the lines appear on the
PC. PCSX2 takes writes to stdout itself, so use `hosttest`, which also writes to
`tty0:` explicitly, when testing in the emulator.
