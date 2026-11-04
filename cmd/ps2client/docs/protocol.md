# ps2link protocol

Wire protocol between a PC and ps2link (tested against ps2link v1.9.3).

All integers are big-endian. Every packet starts with a 6-byte header:

| Field | Size | Meaning |
|---|---|---|
| `cmd` | u32 | Opcode |
| `len` | u16 | Total packet length including this header |

## Channels

| Purpose | Transport | Port | Direction |
|---|---|---|---|
| Commands | UDP | 0x4712 (18194) on the PS2 | PC to PS2, one datagram per command |
| File requests | TCP | 0x4711 (17681) on the PS2 | PC connects; the PS2 sends requests, the PC replies |
| Console text | UDP | 0x4712 on the PC | PS2 to PC, broadcast raw text (udptty sends to 255.255.255.255) |

The PS2 is the TCP server for file requests, but it is also the requester: after
the PC connects, the PS2 sends `open`, `read`, ... over that connection whenever
a program touches `host:`, and the PC answers each one. A new connection
replaces the previous one. ps2link remembers the address of the connecting PC.

## Commands (UDP)

Fixed-size packets. `len` is the size of the struct. Path fields are 256 bytes,
NUL-terminated, zero padded.

| Opcode | Name | Payload after header |
|---|---|---|
| 0xBABE0201 | reset | none |
| 0xBABE0202 | execiop | i32 argc, 256 bytes: `path\0arg1\0arg2\0...`. Packet length must be exactly 266 or it is ignored |
| 0xBABE0203 | execee | i32 argc, 256 bytes, same layout |
| 0xBABE0204 | poweroff | none |
| 0xBABE0205 | scrdump | none (exceptions are shown on screen) |
| 0xBABE0206 | netdump | none (exceptions are sent over the network) |
| 0xBABE0207 | dumpmem | u32 offset, u32 size, 256-byte path: the PS2 writes memory to that path on `host:` |
| 0xBABE0208 | startvu | i32 unit |
| 0xBABE0209 | stopvu | i32 unit |
| 0xBABE020A | dumpreg | i32 register group, 256-byte path: the PS2 writes the registers to that path |
| 0xBABE020B | gsexec | u16 size, 256-byte path: the PS2 reads the file and sends it to the GS |
| 0xBABE020C | writemem | u32 offset, u32 size, 256-byte path: the PS2 reads the file into memory |
| 0xBABE020D | (unused) | not implemented by this tool |

`argc` counts the path as the first argument. Register groups: 0 DMA, 1 INTC,
2 timer, 3 GS, 4 SIF, 5 FIFO, 6 GIF, 7 VIF0, 8 VIF1, 9 IPU, 10 all, 11 VU0, 12 VU1.

Commands that name a file (`execee`, `dumpmem`, `dumpreg`, `gsexec`,
`writemem`) make the PS2 open that path through `host:`, so the PC must be
serving file requests on the TCP channel for them to work.

## File requests (TCP)

Requests come from the PS2; replies come from the PC. A reply carries the
request opcode with the low nibble 2 instead of 1 (0xBABE0111 open is answered
by 0xBABE0112). Negative return values are errors.

| Request | Opcode | Payload after header | Reply payload |
|---|---|---|---|
| open | 0xBABE0111 | i32 flags, 256-byte path | i32 fd or error |
| close | 0xBABE0121 | i32 fd | i32 result |
| read | 0xBABE0131 | i32 fd, i32 nbytes | i32 result, i32 nbytes, then `nbytes` of data |
| write | 0xBABE0141 | i32 fd, i32 nbytes, then `nbytes` of data (`len` covers the header part only) | i32 bytes written or error |
| lseek | 0xBABE0151 | i32 fd, i32 offset, i32 whence | i32 new position or error |
| opendir | 0xBABE0161 | i32 flags (0), 256-byte path | i32 handle or error |
| closedir | 0xBABE0171 | i32 handle | i32 result |
| readdir | 0xBABE0181 | i32 handle | i32 result (0 at end), u32 mode, u32 attr, u32 size, 3 x 8-byte times, u32 hisize, 256-byte name |
| remove | 0xBABE0191 | 256-byte path | i32 result |
| mkdir | 0xBABE01A1 | i32 mode (little-endian, unlike every other field), 256-byte path | i32 result |
| rmdir | 0xBABE01B1 | 256-byte path | i32 result |
| getstat | 0xBABE01C1 | 256-byte path | i32 result, u32 mode, u32 attr, u32 size, 3 x 8-byte times, u32 hisize |

`read` and `write` are chunked by the PS2 to fit in one 1460-byte segment, so
chunks are at most 1460 minus the header size. The PS2 does not wait for a
`len` field to tell it how much data follows a write: the size comes from
`nbytes`.

Reply `len` is the size of the reply structure: 10 bytes for a plain result,
14 for a read reply (the data that follows is not counted), 306 for a readdir
reply and 50 for a getstat reply. The PS2 accepts a reply whose `len` does not
exceed its buffer and reads exactly `len` bytes.

Flags follow the PS2 fileio values: read 0x1, write 0x2, read/write 0x3,
non-blocking 0x10, append 0x100, create 0x200, truncate 0x400, exclusive 0x800.
`whence`: 0 set, 1 current, 2 end.

Stat modes use the PS2's classic ioman encoding, not the fileXio one: the type
is in bits 0x38 (directory 0x20, regular file 0x10, link 0x08) and permissions
are a single triple (read 0x4, write 0x2, execute 0x1). The fileXio values
(0x1000 directory, 0x2000 file) overlap the ioman type bits once permissions
are added, and a PS2 program then sees every entry as both a file and a
directory.

Times are 8 bytes: unused, second, minute, hour, day, month, year (u16,
little-endian).

## Console text

Plain text datagrams; nothing to parse.

## Behaviour worth knowing

- The device name programs use (`host:` on hardware) is chosen by ps2link's IOP
  driver. PCSX2 reserves `host:` for its own host filesystem, so an emulator run
  needs ps2link built with a different device name.
- A close request is answered with 0, but a PS2 program calling `close()` may get
  the descriptor number back.
- The PS2's `fioRemove` is documented in the SDK as broken: after a successful
  remove it falls through and sends a mkdir for the same path. The server sees a
  remove followed by a mkdir; both are answered normally, so the removed file is
  replaced by an empty directory of the same name.
- `mkdir` always carries mode 0x1ED on the PS2 versions tested.

## ps2netfs (file server on the PS2, TCP port 0x4713)

`ps2client fs` talks to the ps2netfs IOP module over one TCP connection. All
integers are big-endian. Every packet starts with a 6-byte header: u32 opcode
and u16 length. For requests and replies the length is the size of the
structure that follows the header's own 6 bytes plus those 6 bytes; data that
trails a write request or a read reply is not counted. A reply opcode is the
request opcode plus 1 (0xBEEF8011 is answered by 0xBEEF8012). Paths and device
names are 256-byte NUL-padded fields. A request whose length is not the exact
expected size is dropped without a reply and the connection is closed.

| Request | Opcode | Payload after header | Reply payload |
|---|---|---|---|
| open | 0xBEEF8011 | i32 flags, path | i32 fd or error |
| close | 0xBEEF8021 | i32 fd | i32 result |
| read | 0xBEEF8031 | i32 fd, i32 nbytes | i32 result, i32 nbytes, then the data |
| write | 0xBEEF8041 | i32 fd, i32 nbytes, then the data | i32 bytes written or error |
| remove | 0xBEEF8071 | i32 0, path | i32 result |
| mkdir | 0xBEEF8081 | i32 0, path | i32 result |
| rmdir | 0xBEEF8091 | i32 0, path | i32 result |
| dopen | 0xBEEF80A1 | i32 0, path | i32 handle or error |
| dclose | 0xBEEF80B1 | i32 handle | i32 result |
| dread | 0xBEEF80C1 | i32 handle | i32 result (0 at end), u32 mode, u32 attr, u32 size, 3 x 8-byte times, u32 hisize, 256-byte name |
| sync | 0xBEEF8131 | i32 flag, device | i32 result |
| mount | 0xBEEF8141 | driver, block device, i32 flags, 256-byte argument, i32 argument length | i32 result |
| umount | 0xBEEF8151 | i32 0, device | i32 result |
| devlist | 0xBEEF8F21 | i32 0, field whose first byte is the delimiter | i32 result, i32 count, 256-byte delimited list |

Transfers are chunked: the server caps one read or write at 65535 bytes.
Stat modes use the fileXio encoding (directory 0x1000, file 0x2000, link
0x4000), and times use the 8-byte layout of the ps2link section.

A negative result is a negated errno; -19 (no such device) means the device
name in the path is not registered on the PS2. Descriptor and directory handles
share one table of 49 entries.
