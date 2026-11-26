# Bits, bytes and numbers

## Why care?

Here is a real kind of bug. A counter of type `int8` counts how many retries a job did. It reaches 127. One more retry, and the counter says **-128**. The job now thinks it has a negative number of retries, and the retry logic goes crazy.

Nothing crashed. No error was printed. The computer did exactly what it was told. To understand _why_, you need to know how numbers are stored.

## The idea: everything is bits

A **bit** is one tiny switch. It is `0` or `1`. That is all a computer can store.

One bit is not much. So we group them:

- **8 bits = 1 byte.** The byte is the smallest unit that has its own address in memory.
- 2 bytes = 16 bits, 4 bytes = 32 bits, 8 bytes = 64 bits. A "64-bit CPU" works on 64 bits at once.

The bits themselves have **no meaning**. The byte `01000001` can be:

- the number 65,
- the letter `A`,
- part of a pixel color,
- part of a machine instruction.

The meaning comes only from the code that reads it. Keep this in mind for the whole chapter: **bits + a rule for reading them = data.**

## Binary: counting with only two fingers

In normal (decimal) numbers, each position is worth 10 times the position on its right: ones, tens, hundreds.

In binary, each position is worth **2 times** the position on its right:

| Position value | 128 | 64  | 32  | 16  | 8   | 4   | 2   | 1   |
| -------------- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bit            | 1   | 1   | 0   | 0   | 1   | 0   | 1   | 0   |

Add the values where the bit is `1`: 128 + 64 + 8 + 2 = **202**.

With `n` bits you can make 2ⁿ different patterns:

| Bits | Patterns                 | Unsigned range                  |
| ---- | ------------------------ | ------------------------------- |
| 8    | 256                      | 0 to 255                        |
| 16   | 65,536                   | 0 to 65,535                     |
| 32   | about 4.3 billion        | 0 to 4,294,967,295              |
| 64   | about 18 billion billion | 0 to 18,446,744,073,709,551,615 |

!!! note "Why 4 GB was a limit"

    A 32-bit address can name about 4.3 billion bytes. That is 4 GiB.
    This is why old 32-bit systems could not use more than 4 GB of RAM per program.

## Hexadecimal: binary for humans

Long rows of 0 and 1 are hard to read. **Hexadecimal** (hex, base 16) is the fix. It uses the digits `0-9` and then `A-F` for 10 to 15.

The trick: **one hex digit = exactly 4 bits.** So one byte is always exactly two hex digits.

```text
binary:  1100 1010
hex:        C    A     → 0xCA = 202
```

This is why hex is everywhere: memory addresses, colors (`#FF8800`), hashes, UUIDs, byte dumps. It is not a different kind of number. It is just a short way to write bits.

## Negative numbers: two's complement

How do you store `-1` when you only have `0` and `1`? Almost every CPU today uses **two's complement**. The rule for an 8-bit number:

> The highest bit is worth **-128** instead of +128. All other bits are normal.

| Bits       | Value               |
| ---------- | ------------------- |
| `00000000` | 0                   |
| `00000001` | 1                   |
| `01111111` | 127 (largest)       |
| `10000000` | -128 (smallest)     |
| `11111111` | -128 + 127 = **-1** |

To make a number negative by hand: flip all bits, then add 1.

```text
 5 = 0000 0101
flip 1111 1010
 +1  1111 1011  = -5
```

Why this strange system? Because the CPU can use **the same adding circuit** for signed and unsigned numbers. `5 + (-5)` in bits is `00000101 + 11111011 = 1_00000000`. The extra ninth bit falls off the end, and you get `0`. No special case. Cheap and fast hardware.

## Overflow: the odometer effect

Think of an old car odometer with 3 digits. After 999 comes 000. The number "wraps around".

Fixed-size integers do the same. This is our retry bug:

```text
int8:   0111 1111  (127)
  +1    1000 0000  (-128)   ← the top bit is now set, so it reads as negative
```

In Go, integer overflow **does not panic** and gives **no error**. It silently wraps. (Go does catch overflow of _constants_ at compile time, so `var x int8 = 128` will not compile.)

### Try it in Go

```go
package main

import "fmt"

func main() {
	n := 202
	fmt.Printf("decimal %d = binary %08b = hex %X\n", n, n, n)

	var small int8 = 127
	small++
	fmt.Println("int8 127 + 1 =", small)

	var u uint8 = 0
	u--
	fmt.Println("uint8 0 - 1 =", u)

	var minus1 int8 = -1
	fmt.Printf("int8 -1 as bits: %08b\n", uint8(minus1))
}
```

Output:

```text
decimal 202 = binary 11001010 = hex CA
int8 127 + 1 = -128
uint8 0 - 1 = 255
int8 -1 as bits: 11111111
```

Look at the last line. `-1` and `255` are **the same bits**. Only the type (the rule for reading) is different.

## Go integer types

| Type               | Size                      | Notes                                                  |
| ------------------ | ------------------------- | ------------------------------------------------------ |
| `int8` … `int64`   | 1, 2, 4, 8 bytes          | Signed, two's complement                               |
| `uint8` … `uint64` | 1, 2, 4, 8 bytes          | Unsigned. `byte` is another name for `uint8`           |
| `int`, `uint`      | 8 bytes on 64-bit systems | The default. Use `int` unless you have a reason not to |
| `uintptr`          | size of a pointer         | For low-level code only                                |
| `rune`             | 4 bytes                   | Another name for `int32`. Holds one Unicode code point |

!!! warning "Trap: `uint` for things that \"can never be negative\""

    It is tempting to use `uint` for a length or a count. But `len(x) - 1` on an empty slice
    becomes a giant number instead of `-1`, and a loop like `for i := n - 1; i >= 0; i--` with
    an unsigned `i` never ends, because `i >= 0` is always true. Go itself uses `int` for `len()`.
    Follow it.

## Bitwise operators

Sometimes you want to work on single bits. Go has these operators:

```go
// 6 = 110, 3 = 011
6 & 3   // 010 = 2   AND: 1 where both are 1
6 | 3   // 111 = 7   OR: 1 where at least one is 1
6 ^ 3   // 101 = 5   XOR: 1 where they differ
6 &^ 3  // 100 = 4   AND NOT (bit clear): remove the bits of 3 from 6
5 << 1  // 1010 = 10 shift left: times 2
5 >> 1  // 10 = 2    shift right: divide by 2
```

A common use in real code is **flags**: many yes/no options packed in one integer. The standard library does this, for example `os.O_CREATE|os.O_WRONLY` or file permission bits like `0o644`.

```go
type Permission uint8

const (
	Read Permission = 1 << iota // 0001
	Write                       // 0010
	Delete                      // 0100
)

func main() {
	p := Read | Write           // give two permissions
	fmt.Println(p&Write != 0)   // true: has Write?
	p &^= Write                 // remove Write
	fmt.Println(p&Write != 0)   // false
}
```

## Floating point: why 0.1 + 0.2 is not 0.3

Integers are exact. Fractions are a different story. Go's `float64` uses the **IEEE 754** standard. A float64 is 64 bits split in three parts:

```text
| sign (1 bit) | exponent (11 bits) | fraction (52 bits) |
```

It is like scientific notation (`1.5 × 10³`), but with base 2. The problem: many simple decimal numbers like `0.1` have **no exact binary form**. It is like `1/3` in decimal: `0.3333…` never ends. So the computer stores the closest number it can.

```go
a, b := 0.1, 0.2
fmt.Println(a+b == 0.3, a+b)
fmt.Println(0.1+0.2 == 0.3)
```

Output:

```text
false 0.30000000000000004
true
```

Wait, why is the second line `true`? Because `0.1+0.2` written with constants is computed by the **compiler** with exact, very high precision math. Only when values go into `float64` variables do you get the real hardware behavior. This difference surprises many Go developers.

!!! danger "Never use floats for money"

    Rounding errors add up. After many operations, cents go missing.
    Store money as an integer of the smallest unit (`int64` cents), or use a decimal library.
    Compare floats with a tolerance: `math.Abs(a-b) < 1e-9`, never with `==`.

## Check yourself

??? question "1. What is `0xFF` in decimal? And in binary?"

    255. Binary `1111 1111`. Two hex digits = one byte, and `F` = `1111`.

??? question "2. A `uint8` has the value 250. You add 10. What is the result?"

    4. The real result 260 does not fit; 260 - 256 = 4. It wraps around.

??? question "3. Why can `int8` hold -128 but only +127?"

    There are 256 patterns. One of them is zero. That leaves 255 for the rest, so one side
    gets one more. In two's complement, the negative side gets it.

??? question "4. Your team stores prices as `float64`. What do you say?"

    Floats cannot store most decimal fractions exactly, so sums drift. Use integer cents
    (`int64`) or a decimal type.

## Recap

- Everything is bits. The **type** gives them meaning.
- Hex is a short way to write bits: one hex digit = 4 bits.
- Negative integers use two's complement. The top bit is the sign.
- Integer overflow in Go wraps silently. Pick sizes with care, and prefer `int`.
- Floats are approximations. Never compare them with `==`, never use them for money.
