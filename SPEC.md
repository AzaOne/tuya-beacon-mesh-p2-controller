# Tuya Beacon Mesh P2 lamp command format

This specification describes the Smart Bulb with Tuya product ID `wbl39qpt`. It is based on Smart Life BLE advertisements and Android HCI BTSnoop captures. A phone or Linux controller sends commands by advertising them over BLE. The protocol provides no acknowledgment that the lamp applied a command.

An `on` command passes through three layers. The controller encodes it in a **16-byte datapoint block**, places the encrypted block in a **26-byte P2 frame**, and broadcasts the frame in **31 bytes of BLE advertising data**. All offsets below are zero-based. Byte examples use hexadecimal.

## 1. Datapoint block (16 bytes)

Each lamp property has a datapoint (DP) ID. A datapoint entry consists of the ID, one byte encoding both type and length, and the value. The `on` entry takes three bytes:

```text
01   11   01
│    │    └─ Value 1 (on)
│    └────── High nibble 1 = boolean, low nibble 1 = one byte of value
└─────────── DP 1 (power)
```

With `multi_dp=true`, the controller packs consecutive entries into the block. If the next entry would exceed 16 bytes, it starts a new block with that entry. Unused bytes are zero-filled. A zero at the next DP ID position starts the padding. A zero inside a value is part of the value.

| DP (decimal) | Property | Value encoding | Complete entry (hex) |
| --- | --- | --- | --- |
| 1 | Power | Type 1, one byte, 0 or 1 | `01 11 00` — off |
| 2 | Mode | Type 4, one byte, 0 for white, 1 for color, 2 for scene | `02 41 01` — color |
| 3 | Brightness | Type 2, big-endian uint32, range 10–1000 | `03 24 00 00 03 E8` — maximum |
| 4 | Color temperature | Type 2, big-endian uint32, range 0–1000 | `04 24 00 00 03 E8` — upper end of the scale |
| 11 | HSV color | Type 0, two-byte big-endian hue followed by one byte each for saturation and value in percent | `0B 04 00 78 64 64` — green (120°, 100%, 100%) |
| 73 | Scene | Type 0, raw preset bytes | `49 08 08 00 02 00 3C 64 40 80` — Warm light breath |

The CLI converts RGB to HSV and maps kelvin values onto the DP4 scale using the configured endpoints. The actual color temperature depends on lamp calibration. DP2 value 3 (`music`) is experimental.

### Reading a DP73 scene

In the scene entry above, `49 08` identifies DP73 and gives the value length of eight bytes. Within the value `08 00 02 00 3C 64 40 80`, byte 0 selects scene 8, byte 4 sets the speed to 60%, and byte 5 sets the brightness to 100%. Captures of the same scene at different settings confirm the meanings of bytes 4 and 5. The controller copies the remaining bytes from the scene template because their meanings are only partially understood.

For `scene 8`, the controller sends a single DP73 entry. Run `tuya-beacon-mesh-p2-controller scene list` to see the names, IDs, and raw bytes of all 28 presets. When the monitor has saved a valid value from the phone for the selected scene, the controller uses that value as its template. [Scene catalog and implementation](internal/p2/scenes.go)

## 2. P2 frame (26 bytes)

Offsets in this table start at the first byte of the **P2 frame**. The controller writes `src`, `dst`, and `seq` in big-endian order. The two address fields identify the transmitter and lamp within the mesh. The Bluetooth MAC address and cloud `device_id` are absent from the frame header.

| Bytes | Field | Contents |
| --- | --- | --- |
| 0 | Header | `0B` for the observed commands |
| 1–2 | `src` | Transmitter address |
| 3–4 | `dst` | Lamp address |
| 5–6 | `seq` | 16-bit sequence number |
| 7 | Subcommand | `05` for a datapoint block |
| 8 | Block checksum | CRC16HI over all 16 plaintext bytes, including padding |
| 9–24 | Payload | 16 bytes after encryption and XOR masking |
| 25 | Frame checksum | CRC16HI over the adjusted header and ciphertext **before XOR masking**, as defined below |

Let `P` be the 16-byte datapoint block, `K1` the 16 bytes of `local_key`, and `K2` the 16 bytes of `beacon_key`. In the configuration, a 16-character ASCII key represents its literal bytes. A 32-character hexadecimal key decodes to 16 bytes.

1. Fill bytes 0–7 and write `CRC16HI(P)` to byte 8.
2. Encrypt `P` with `K1` using XXTEA. Treat the block and key as four big-endian 32-bit words each. Use 19 rounds and the constant `0x9E3779B9`. Call the resulting 16 bytes `E`.
3. Compute byte 25 as CRC16HI over the following 25 bytes in order: `(frame[0] & 0xFC)`, bytes 1–8 inclusive, then `E`. For header byte `0B`, the first byte of the checksum input is `08`.
4. Write the bytewise result of `E XOR K2` to bytes 9–24.

I learned about the role of `beacon_key` as the XOR mask from [oLucasAguilar](https://github.com/oLucasAguilar)’s [tuya-beacon-linux](https://github.com/oLucasAguilar/tuya-beacon-linux).

**CRC16HI** starts with a 16-bit state `s = 0`. For each input byte, apply `s ^= byte << 8`, then perform eight iterations. In each iteration, apply `s ^= 0x8380` if bit 15 is set. Always shift `s` one bit to the left, retaining the low 16 bits. Return the high byte of the final state. [Frame construction, XXTEA, and CRC16HI](internal/p2/protocol.go)

## 3. BLE advertising data (31 bytes)

The advertisement contains two BLE advertising data structures. The first carries the flags. The second uses type `03` (Complete List of 16-bit Service UUIDs) to carry the P2 frame. This is why `btmon` may display **13 UUIDs**. Their 26 bytes in the original on-air order form the frame.

| Advertising data offsets | Bytes | Meaning |
| --- | --- | --- |
| 0–2 | `02 01 01` | Length 2, type Flags, observed value `01` |
| 3–4 | `1B 03` | Length 27, covering one type byte and 26 data bytes |
| 5–30 | P2 frame | The complete frame from the previous section |

### Worked example: switching the lamp on

This example uses synthetic inputs: `src=0x7DF9`, `dst=0x0005`, `seq=0x1234`, `K1=ASCII("0123456789abcdef")`, and `K2=0102030405060708090a0b0c0d0e0f10`.

```text
DP block (16)      01 11 01 00 00 00 00 00 00 00 00 00 00 00 00 00
Header (0–8)       0B 7D F9 00 05 12 34 05 33
Payload (9–24)     EA 17 C6 A5 73 42 6C 64 2B AB 62 66 A6 47 9C 3F
Checksum (25)      F6

BLE ADV (31)       02 01 01 1B 03 0B 7D F9 00 05 12 34 05 33
                   EA 17 C6 A5 73 42 6C 64 2B AB 62 66 A6 47 9C 3F F6
```

Here `33` checks the plaintext block and `F6` checks the adjusted header and unmasked ciphertext. To inspect the bytes for your own configuration, use `--dry-run on`. It prints the P2 frame as `frame=` and the complete advertising data as `adv=` without advancing the sequence counter.

## Receiving commands and tracking state

The monitor looks for a BLE structure of type `03` with 26 data bytes. It checks the header, subcommand, and expected addresses, then removes the XOR mask with K2 and verifies the frame checksum. Next it decrypts the block with K1 and verifies the plaintext checksum. Once both checks pass, it parses the datapoints and can update the local state from the observed phone command. [Frame decoder](internal/p2/decode.go)

Before transmission, the controller reserves consecutive `seq` values in the state file. A reserved number remains consumed even if the adapter reports an error. The monitor and `seq sync-log` can advance the counter using verified frames from the phone. The controller stops reserving numbers if a reservation would exceed `0xFFFF`. Firmware behavior at wraparound and the use of separate counters for different sources remain unknown. [Sequence reservation](internal/state/state.go)

Pairing the lamp again may change its keys and mesh addresses. Both `learn live` and `learn capture.cfa` use the current K1 and verify the recovered K2 against distinct commands with different sequence numbers. By default, they save the verified key to `device.beacon_key`. [Obtaining the keys](README.md#obtain-the-keys)

## Verification

- **Smart Life capture.** All 84 decoded frames in the Android capture matched the originals byte for byte when re-encoded. This result covers the commands present in that capture. [Private capture regression test](internal/capture/improvements_test.go)
- **Project tests.** A synthetic test vector checks frame encoding independently of real keys. Additional tests check the bytes for on/off, mode, color temperature, brightness, and HSV commands. Run `go test ./internal/p2 ./internal/capture`. [Protocol test vectors](internal/p2/protocol_test.go)
- **Physical lamp.** The lamp was observed switching off in response to the OFF command. Support for arbitrary combinations of datapoints in one block and for alternative BLE flag values depends on the firmware. A successful HCI transmission confirms that the adapter accepted the command.
