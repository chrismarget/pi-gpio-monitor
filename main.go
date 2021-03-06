package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/stianeikeland/go-rpio"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type (
	inPinNameArgs  []string
	inPinStateArgs []string
	pinDescription map[uint8]string
	pinStates      map[rpio.State]string
)

type pinState struct {
	pin   rpio.Pin
	state rpio.State
}

type config struct {
	pinDescription pinDescription
	pinStates      pinStates
	port           int
	interval       int
	debug          bool
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
		err = fmt.Errorf("values > %d not supported", math.MaxUint8)
	}
	return uint8(i), parts[1], err
}

// ParseSCLI calls the flag library and parses the result. It returns maps
// of pin descriptions (by pin number) and pin state names (by rpio.State)
func parseCLI() (config, error) {
	var inPinName inPinNameArgs
	var inPinState inPinStateArgs
	flag.Var(&inPinName, "n", "Pin number : name, like this: '23:Thing attached to pin 23' ")
	flag.Var(&inPinState, "s", "Pin state : description, like this: '1:Pin enabled' ")
	tcpPort := flag.Int("l", -1, "tcp listen port")
	interval := flag.Int("i", 100, "polling interval (ms) default: 100")
	debug := flag.Bool("d", false, "debug to stdErr")
	flag.Parse()

	pd := make(map[uint8]string)
	for _, v := range inPinName {
		pinNum, name, err := splitArgString(v)
		if err != nil {
			return config{}, err
		}
		pd[pinNum] = name
	}

	ps := make(map[rpio.State]string)
	ps[0] = "off"
	ps[1] = "on"
	for _, v := range inPinState {
		state, description, err := splitArgString(v)
		if err != nil {
			return config{}, err
		}
		ps[rpio.State(state)] = description
	}
	return config{
		pinDescription: pd,
		pinStates:      ps,
		port:           *tcpPort,
		interval:       *interval,
		debug:          *debug,
	}, nil
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
func monitor(pins []rpio.Pin, out chan pinState, interval int) {
	status := make(map[rpio.Pin]rpio.State)
	for {
		for _, pin := range pins {
			if update(pin, status) {
				out <- pinState{
					pin:   pin,
					state: status[pin],
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	config, err := parseCLI()
	if err != nil {
		log.Fatal(err)
	}

	err = rpio.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer rpio.Close()

	clients := make(map[string]net.Conn)
	var clientLock sync.Mutex
	if config.port > 0 {
		l, err := net.ListenTCP("tcp", &net.TCPAddr{Port: config.port})
		if err != nil {
			log.Fatal(err)
		}
		defer l.Close()

		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					log.Println(err)
					continue
				}
				clientLock.Lock()
				clients[c.LocalAddr().String()+" - "+c.RemoteAddr().String()] = c
				clientLock.Unlock()
			}
		}()
	}

	//pin := make(map[rpio.Pin]uint8)
	var pins []rpio.Pin
	for i, _ := range config.pinDescription {
		p := rpio.Pin(i)
		p.Input()
		p.PullUp()
		pins = append(pins, p)
	}

	changes := make(chan pinState)
	go monitor(pins, changes, 100)

	for {
		change := <-changes
		msg := fmt.Sprintf("%s changed state to %s\n",
			config.pinDescription[uint8(change.pin)],
			config.pinStates[change.state])
		clientLock.Lock()
		for k, v := range clients {
			_, err := v.Write([]byte(msg))
			if err != nil {
				log.Println(err)
				err = v.Close()
				if err != nil {
					log.Println(err)
				}
				clients[k] = nil
				delete(clients, k)
			}
		}
		clientLock.Unlock()
	}
}
