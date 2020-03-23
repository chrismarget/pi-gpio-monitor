package main

import (
	"flag"
	"github.com/getlantern/errors"
	"github.com/stianeikeland/go-rpio"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

type (
	inPinNameArgs  []string
	inPinStateArgs []string
	pinDescription map[uint8]string
	pinStateName   map[rpio.State]string
	pinName        map[rpio.Pin]string
)

type pinState struct {
	pinNum uint8
	state  rpio.State
}

// String is required to fulfill the flag.Value interface
func (o *inPinNameArgs) String() string {
	return ""
}

// Set is required to fulfill the flag.Value interface
func (o *inPinNameArgs) Set(value string) error {
	*o = append(*o, value)
	return nil
}

// String is required to fulfill the flag.Value interface
func (i *inPinStateArgs) String() string {
	return ""
}

// Set is required to fulfill the flag.Value interface
func (o *inPinStateArgs) Set(value string) error {
	*o = append(*o, value)
	return nil
}

// splitArgString expects a string formatted like "xxx:stuff" where "xxx" is
// string representation of an integer 0 - 255 and "stuff" is a string. It
// returns the input as a uint8 and a string.
func splitArgString(in string) (uint8, string, error) {
	parts := strings.SplitN(in, ":", 2)
	if len(parts) != 2 {
		return 0, "", errors.New("argument '%s' not delimited by ':'")
	}
	i, err := strconv.Atoi(parts[0])
	if i > math.MaxUint8 {
		err = errors.New("values > %d not supported", math.MaxUint8)
	}
	return uint8(i), parts[1], err
}

// ParseSCLI calls the flag library and parses the result. It returns maps
// of pin descriptions (by pin number) and pin state names (by rpio.State)
func parseCLI() (pinDescription, pinStateName, error) {
	var inPinName inPinNameArgs
	var inPinState inPinStateArgs
	flag.Var(&inPinName, "n", "Pin number : name, like this: '23:Thing attached to pin 23' ")
	flag.Var(&inPinState, "s", "Pin state : description, like this: '1:Pin enabled' ")
	flag.Parse()

	pd := make(map[uint8]string)
	for _, v := range inPinName {
		pinNum, name, err := splitArgString(v)
		if err != nil {
			return nil, nil, err
		}
		pd[pinNum] = name
	}

	ps := make(map[rpio.State]string)
	ps[0] = "off"
	ps[1] = "on"
	for _, v := range inPinState {
		state, description, err := splitArgString(v)
		if err != nil {
			return nil, nil, err
		}
		ps[rpio.State(state)] = description
	}
	return pd, ps, nil
}

// update returns true if the pin status has changed.
func update(pin rpio.Pin, status map[rpio.Pin]rpio.State) bool {
	nowState := pin.Read()
	if oldState, exists := status[pin]; !exists {
		// previously unknown state
		status[pin] = nowState
		return false
	} else {
		// previously known state
		if nowState == oldState {
			// no change
			return false
		} else {
			// state change
			status[pin] = nowState
			return true
		}
	}
}

// monitor loops forever checking state of the pins, writes
// state changes to the 'out' channel.
func monitor(pin map[rpio.Pin]uint8, out chan pinState) {
	status := make(map[rpio.Pin]rpio.State)
	for {
		for pin, pinNum := range pin {
			if update(pin, status) {
				out <- pinState{
					pinNum: pinNum,
					state:  status[pin],
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	pinDescription, pinStateName, err := parseCLI()
	if err != nil {
		log.Fatal(err)
	}

	err = rpio.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer rpio.Close()

	pin := make(map[rpio.Pin]uint8)
	for i, _ := range pinDescription {
		p := rpio.Pin(i)
		p.Input()
		p.PullUp()
		pin[p] = i
	}

	changes := make(chan pinState)
	go monitor(pin, changes)

	for {
		change := <-changes
		log.Printf("%s changed state to %s\n", pinDescription[change.pinNum], pinStateName[change.state])
	}
}
