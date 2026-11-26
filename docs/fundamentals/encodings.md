# Encodings

## Why care?

A customer signs up with the name `José`. Your API checks "name must be at most 4 characters" with `len(name) <= 4`. José is rejected. Another service cuts names to 10 bytes and the database now contains broken text like `Jos\xc3`.

Both bugs come from one fact: **text is not characters. Text is bytes, plus a rule (an encoding) for turning bytes into characters.**

## The idea: a codebook

An **encoding** is a codebook. It says "this number means this character". Writer and reader must use the same codebook. If they don't, you get garbage like `JosÃ©`. This garbage even has a name: _mojibake_.

## ASCII: the old American codebook

ASCII (1963) uses 7 bits, so it has 128 codes:

| Code   | Character                                  |
| ------ | ------------------------------------------ |
| 0–31   | Control codes, like `\n` (10) and `\t` (9) |
| 32     | Space                                      |
| 48–57  | `0`–`9`                                    |
| 65–90  | `A`–`Z`                                    |
| 97–122 | `a`–`z`                                    |

Two nice tricks are built in:

- `'a' - 'A'` = 32. Upper and lower case differ by **one bit** (`0x20`).
- `'7' - '0'` = 7. That is how you turn a digit character into its number.

The problem: ASCII has no `é`, no `ß`, no `中`, no `👋`. Many countries made their own 8-bit codebooks, all different. Files moved between countries became garbage.

## Unicode: one list for every character

**Unicode** fixes this with one giant list. Every character in every language gets a unique number called a **code point**. We write it as `U+` plus hex:

| Character | Code point |
| --------- | ---------- |
| `A`       | U+0041     |
| `é`       | U+00E9     |
| `€`       | U+20AC     |
| `中`      | U+4E2D     |
| `👋`      | U+1F44B    |

Code points go up to U+10FFFF, so there is room for about 1.1 million characters.

!!! info "Unicode is not an encoding"

    Unicode says _which number_ each character has. It does **not** say how to store that number
    in bytes. That is the job of an encoding like UTF-8, UTF-16 or UTF-32.

## UTF-8: the encoding that won

You could store every code point in 4 bytes (that is UTF-32). Simple, but English text becomes 4 times bigger. **UTF-8** is smarter. It uses 1 to 4 bytes, depending on the code point:

| Code point range   | Bytes | Bit pattern                           |
| ------------------ | ----- | ------------------------------------- |
| U+0000 – U+007F    | 1     | `0xxxxxxx`                            |
| U+0080 – U+07FF    | 2     | `110xxxxx 10xxxxxx`                   |
| U+0800 – U+FFFF    | 3     | `1110xxxx 10xxxxxx 10xxxxxx`          |
| U+10000 – U+10FFFF | 4     | `11110xxx 10xxxxxx 10xxxxxx 10xxxxxx` |

The `x` bits hold the code point. Look how clever this design is:

- **ASCII is valid UTF-8.** Every old ASCII file is already UTF-8. That is why UTF-8 won the web.
- **You always know where you are.** A byte starting with `10` is a "continuation" byte. Any other byte starts a new character. If you jump into the middle of a text, you can find the next character start.
- **No zero bytes** inside multi-byte characters, so old C code that uses `0` as "end of string" still works.

Let's encode `é` (U+00E9) by hand:

```text
U+00E9       = 000 1110 1001  (11 bits needed → 2-byte form)
pattern        110xxxxx 10xxxxxx
fill in bits   11000011 10101001
hex            C3       A9
```

So `é` is the two bytes `C3 A9`. Now you can explain the mojibake `Ã©`: a program read `C3 A9` with the old Latin-1 codebook, where `C3` is `Ã` and `A9` is `©`.

## Strings in Go: bytes, runes and characters

Go source code is always UTF-8. A Go `string` is a **read-only slice of bytes**. It is _usually_ UTF-8, but Go does not force it.

There are three different "sizes" of one piece of text:

| Question                          | Go code                     | Result for `"héllo"` |
| --------------------------------- | --------------------------- | -------------------- |
| How many bytes?                   | `len(s)`                    | 6                    |
| How many code points (runes)?     | `utf8.RuneCountInString(s)` | 5                    |
| How many characters a human sees? | needs a library (graphemes) | 5                    |

### Try it in Go

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "héllo"
	fmt.Println(len(s), utf8.RuneCountInString(s))
	fmt.Printf("% x\n", s)

	for i, r := range s {
		fmt.Printf("%d:%c(U+%04X) ", i, r, r)
	}
	fmt.Println()

	fmt.Println(s[1], s[:2])
	fmt.Println(string([]rune(s)[1]))
}
```

Output:

```text
6 5
68 c3 a9 6c 6c 6f
0:h(U+0068) 1:é(U+00E9) 3:l(U+006C) 4:l(U+006C) 5:o(U+006F)
195 h�
é
```

Read this output slowly. It shows four important rules:

1. `len` counts **bytes**, not characters.
2. `for range` over a string walks **runes** (code points). Look at the index: it jumps from 1 to 3, because `é` uses bytes 1 and 2.
3. `s[1]` gives one **byte** (195 = `0xC3`), not a character.
4. Slicing a string slices **bytes**. `s[:2]` cuts `é` in half, and you get `�`.

If you need "the n-th character", convert to `[]rune` first. That copies the text into 4 bytes per rune, so it costs memory. Do it only when you need it.

!!! warning "Trap: what a human calls one character"

    Some characters a human sees as one are **several code points**. The flag `🇳🇱` is two
    code points. `é` can also be written as `e` + a combining accent (U+0065 U+0301). Both look
    the same but are different bytes. For "user-visible length", use a grapheme library. For
    comparing user input, normalize first with `golang.org/x/text/unicode/norm`.

### Invalid UTF-8

A Go string can hold any bytes. When `range` meets a broken sequence, it gives you `utf8.RuneError` (U+FFFD, shown as `�`) and moves one byte forward:

```go
bad := string([]byte{0x68, 0xff, 0x69})
fmt.Println(utf8.ValidString(bad)) // false
for _, r := range bad {
	fmt.Printf("%q ", r)        // 'h' '�' 'i'
}
```

In enterprise code, check input at the edge of your system (HTTP handlers, file readers) with `utf8.Valid`, and reject or clean it there. `encoding/json` replaces invalid bytes with U+FFFD when it writes JSON.

## Endianness: which byte goes first?

A number bigger than one byte needs several bytes. In what **order** do you store them? Take the 32-bit number `0x0A0B0C0D`:

```text
address:          0    1    2    3
big-endian:      0A   0B   0C   0D    ← "big end" first, like we write numbers
little-endian:   0D   0C   0B   0A    ← "little end" first
```

- **Little-endian**: x86-64 and (normally) ARM. So almost every computer and phone you use.
- **Big-endian**: the standard for network protocols. That's why it is also called **network byte order**.

Inside one program you never notice. It matters when bytes **leave** the program: files, network packets, binary protocols. Then you must say the order explicitly. Go's `encoding/binary` makes this easy:

```go
buf := make([]byte, 4)

binary.BigEndian.PutUint32(buf, 0x0A0B0C0D)
fmt.Printf("big:    % x\n", buf) // 0a 0b 0c 0d

binary.LittleEndian.PutUint32(buf, 0x0A0B0C0D)
fmt.Printf("little: % x\n", buf) // 0d 0c 0b 0a
```

!!! note "UTF-8 has no endianness"

    UTF-8 works one byte at a time, so byte order does not matter. UTF-16 works with 2-byte
    units, so it needs a "byte order mark" (BOM) to say which order it uses. One more reason UTF-8 is the simple choice.

## Other encodings you will meet

Not all encodings are for text. Some turn **any bytes** into safe text:

| Encoding     | What it does                               | Size cost | Where you see it       |
| ------------ | ------------------------------------------ | --------- | ---------------------- |
| Hex          | Every byte → 2 characters `0-9a-f`         | 2×        | Hashes, debug dumps    |
| Base64       | Every 3 bytes → 4 characters `A-Za-z0-9+/` | ~1.33×    | JWTs, email, data URLs |
| URL encoding | Unsafe characters → `%XX`                  | varies    | Query strings          |

In Go: `encoding/hex`, `encoding/base64` (use `URLEncoding` or `RawURLEncoding` for URLs and JWTs), and `net/url`.

!!! danger "Encoding is not encryption"

    Base64 hides nothing. Anyone can decode it. Never "protect" a password or token with Base64.

## Check yourself

??? question "1. How many bytes is `\"€\"` in UTF-8? Why?"

    3 bytes (`e2 82 ac`). U+20AC is in the range U+0800–U+FFFF, which uses the 3-byte form.

??? question "2. Why is `len(\"👋\")` equal to 4?"

    U+1F44B is above U+FFFF, so UTF-8 needs 4 bytes. `len` counts bytes.

??? question "3. You see `Ã©` in your database. What happened?"

    UTF-8 bytes (`C3 A9` for `é`) were decoded as Latin-1 / Windows-1252 somewhere.
    Find the place where the wrong codebook is used (often a DB connection or HTTP header setting).

??? question "4. You send a `uint32` over TCP from an x86 server to an ARM device. Do you need to think about byte order?"

    Yes. Always pick one order in the protocol (usually big-endian) and use
    `binary.BigEndian` on both sides. Do not depend on what the machine does.

## Recap

- An encoding is a codebook from numbers to characters. Writer and reader must agree.
- Unicode gives each character a code point. UTF-8 stores code points in 1–4 bytes.
- In Go: `string` = bytes, `rune` = code point, `len` = bytes, `range` = runes.
- Endianness is byte order for multi-byte numbers. It matters on the wire, not in memory.
- Base64 and hex turn bytes into text. They are not security.
