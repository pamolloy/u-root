// Copyright 2012-2023 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9

// free reports usage information for physical memory and swap space.
//
// Synopsis:
//
//	free [-k] [-m] [-g] [-t] [-h] [-json]
//
// Description:
//
//	Read memory information from /proc/meminfo and display a summary for
//	physical memory and swap space. The unit options use powers of 1024.
//
// Options:
//
//	-k: display the values in kibibytes
//	-m: display the values in mebibytes
//	-g: display the values in gibibytes
//	-t: display the values in tebibytes
//	-h: display the values in human-readable form
//	-json: use JSON output
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
)

var (
	humanOutput = flag.Bool("h", false, "Human output: show automatically the shortest three-digits unit")
	inBytes     = flag.Bool("b", false, "Express the values in bytes")
	inKB        = flag.Bool("k", false, "Express the values in kibibytes (default)")
	inMB        = flag.Bool("m", false, "Express the values in mebibytes")
	inGB        = flag.Bool("g", false, "Express the values in gibibytes")
	inTB        = flag.Bool("t", false, "Express the values in tebibytes")
	toJSON      = flag.Bool("json", false, "Use JSON for output")
)

type unit uint

const (
	// B is bytes
	B unit = 0
	// KB is kibibytes
	KB = 10
	// MB is mebibytes
	MB = 20
	// GB is gibibytes
	GB = 30
	// TB is tebibytes
	TB = 40
)

var units = [...]string{"B", "K", "M", "G", "T"}

var errMultipleUnits = fmt.Errorf("multiple unit options doesn't make sense")

// the following types are used for JSON serialization
type mainMemInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Free      uint64 `json:"free"`
	Shared    uint64 `json:"shared"`
	Cached    uint64 `json:"cached"`
	Buffers   uint64 `json:"buffers"`
	Available uint64 `json:"available"`
}

type swapInfo struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
}

// MemInfo represents the main memory and swap space information in a structured
// manner, suitable for JSON encoding.
type MemInfo struct {
	Mem  mainMemInfo `json:"mem"`
	Swap swapInfo    `json:"swap"`
}

type meminfomap map[string]uint64

// missingRequiredFields checks if any of the specified fields are present in
// the input map.
func missingRequiredFields(m meminfomap, fields []string) bool {
	for _, f := range fields {
		if _, ok := m[f]; !ok {
			log.Printf("Missing field '%v'", f)
			return true
		}
	}
	return false
}

// humanReadableValue returns a string representing the input value, treated as
// a size in bytes, interpreted in a human readable form. E.g. the number 10240
// woud return the string "10 kB". Note that the decimal part is truncated, not
// rounded, so the values are guaranteed to be "at least X"
func humanReadableValue(value uint64) string {
	v := value
	// bits to shift. 0 means bytes, 10 means kB, and so on. 40 is the highest
	// and it means tB
	var shift uint
	for shift < uint(len(units)*10) {
		if v/1024 < 1 {
			break
		}
		v /= 1024
		shift += 10
	}
	var decimal uint64
	if shift > 0 {
		// no rounding. Is there a better way to do this?
		decimal = ((value - (value >> shift << shift)) >> (shift - 10)) * 1000 / 1024 / 100
	}
	return fmt.Sprintf("%v.%v%v",
		value>>shift,
		decimal,
		units[shift/10],
	)
}

// formatValueByConfig formats a size in bytes in the appropriate unit,
// depending on whether FreeConfig specifies a human-readable format or a
// specific unit
func (c *cmd) formatValueByConfig(value uint64) string {
	if c.human {
		return humanReadableValue(value)
	}
	// units and decimal part are not printed when a unit is explicitly specified
	return fmt.Sprintf("%v", value>>c.unit)
}

func main() {
	flag.Parse()
	o := options{human: *humanOutput, bytes: *inBytes, kbytes: *inKB, mbytes: *inMB, gbytes: *inGB, tbytes: *inTB, json: *toJSON}
	cmd, err := command(os.Stdout, o)
	if err != nil {
		log.Fatal(err)
	}
	if err = cmd.run(); err != nil {
		log.Fatal(err)
	}
}

type cmd struct {
	stdout io.Writer
	unit   unit
	human  bool
	toJSON bool
}

type options struct {
	human  bool
	bytes  bool
	kbytes bool
	mbytes bool
	gbytes bool
	tbytes bool
	json   bool
}

func countTrue(b ...bool) int {
	var cnt int
	for _, v := range b {
		if v {
			cnt++
		}
	}
	return cnt
}

func command(stdout io.Writer, o options) (*cmd, error) {
	// validateUnits checks that only one option of -b, -k, -m, -g, -t or -h has been
	// specified on the command line
	count := countTrue(o.bytes, o.kbytes, o.mbytes, o.gbytes, o.tbytes, o.human)
	if count > 1 {
		return nil, errMultipleUnits
	}

	c := &cmd{
		stdout: stdout,
		toJSON: o.json,
	}

	if o.human {
		c.human = true
	} else {
		switch {
		case o.bytes:
			c.unit = B
		case o.mbytes:
			c.unit = MB
		case o.gbytes:
			c.unit = GB
		case o.tbytes:
			c.unit = TB
		default:
			c.unit = KB
		}
	}

	return c, nil
}

// run prints physical memory and swap space information. The fields will be
// expressed with the specified unit (e.g. KB, MB)
func (c *cmd) run() error {
	m, err := meminfo()
	if err != nil {
		return err
	}

	return c.parse(m)
}

func (c *cmd) parse(m meminfomap) error {
	mmi, err := getMainMemInfo(m)
	if err != nil {
		return err
	}
	si, err := getSwapInfo(m)
	if err != nil {
		return err
	}
	mi := MemInfo{Mem: *mmi, Swap: *si}
	if c.toJSON {
		jsonData, err := json.Marshal(mi)
		if err != nil {
			return err
		}
		fmt.Fprintln(c.stdout, string(jsonData))
	} else {
		fmt.Fprintf(c.stdout, "              total        used        free      shared  buff/cache   available\n")
		fmt.Fprintf(c.stdout, "%-7s %11v %11v %11v %11v %11v %11v\n",
			"Mem:",
			c.formatValueByConfig(mmi.Total),
			c.formatValueByConfig(mmi.Used),
			c.formatValueByConfig(mmi.Free),
			c.formatValueByConfig(mmi.Shared),
			c.formatValueByConfig(mmi.Buffers+mmi.Cached),
			c.formatValueByConfig(mmi.Available),
		)
		fmt.Fprintf(c.stdout, "%-7s %11v %11v %11v\n",
			"Swap:",
			c.formatValueByConfig(si.Total),
			c.formatValueByConfig(si.Used),
			c.formatValueByConfig(si.Free),
		)
	}
	return nil
}
