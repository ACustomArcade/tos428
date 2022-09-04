/*
tos428 configures the Switchable 4-to-8-Way Restrictor for Sanwa compatible
Joysticks
*/
package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tarm/serial"
	"github.com/thoas/go-funk"
)

var autoRom string
var devicePath string
var deviceRestrictor string
var exportFile string
var getInfo bool
var rawComand string
var romListPath string
var roms []string
var setWay int

//go:embed roms4way.txt
var romsData []byte

// A GRSDevice is a connection to a tos428
type GRSDevice struct {
	device *serial.Port
}

func (g *GRSDevice) sendCommand(cmd string) {
	_, err := g.device.Write([]byte(cmd))
	if err != nil {
		log.Fatal(err)
	}
}

func (g *GRSDevice) sendCommandWithOutput(cmd string) string {
	g.sendCommand(cmd)
	return g.getOutput()
}

func (g *GRSDevice) getOutput() string {
	buf := make([]byte, 128)
	n, err := g.device.Read(buf)
	if err != nil {
		log.Fatal(err)
	}
	return strings.TrimRight(string(buf[:n]), "\r\n")
}

// DumpEEPROM lists the actual static (EEPROM) memory where configurations are
// permanently stored.
func (g *GRSDevice) DumpEEPROM() {
	g.sendCommand("dumpeeprom")
	r := g.getOutput()
	fmt.Println(r)
}

// GetColor retrieves the actual color code for the modes given in P1
// (4|8|keyboard)
func (g *GRSDevice) GetColor(mode string) (int, int, int) {
	cmd := fmt.Sprintf("getcolor,%s", mode)
	g.sendCommand(cmd)
	r := g.getOutput()

	rgb := strings.Split(r, ",")
	if len(rgb) != 3 {
		log.Fatalf("Invalid response from device %s\n", r)
	}

	var rgbInts []int
	for _, c := range rgb {
		i, err := strconv.Atoi(c)
		if err == nil {
			rgbInts = append(rgbInts, i)
		}
	}

	return rgbInts[0], rgbInts[1], rgbInts[2]
}

func (g *GRSDevice) GetInfo() {
	log.Printf("Device: %s", g.GetWelcome())

	startupWay := g.GetStartupWay()
	log.Printf("Startup Orientation: %d", startupWay)

	red, green, blue := g.GetColor("4")
	log.Printf("4-way Color: %d,%d,%d", red, green, blue)

	red, green, blue = g.GetColor("8")
	log.Printf("8-way Color: %d,%d,%d", red, green, blue)

	red, green, blue = g.GetColor("keyboard")
	log.Printf("Keyboard Color: %d,%d,%d", red, green, blue)

}

// GetKeyList provides a list of supported symbolic key names to the remote
// system (for ConfigTool). Those key names are useful as buttons can be
// configured to act as a USBkeyboard key and send emulated keystrokes for up
// to 3 simultaneously pressed keys
// (e.g. combination KEY_LEFT_CTRL,KEY_LEFT_ALT,KEY_DELETE would be possible.)
func (g *GRSDevice) GetKeyList() []string {
	r := g.sendCommandWithOutput("getkeylist")
	keys := strings.Split(r, "\r\n")
	if len(keys) == 0 {
		log.Fatalln("Unable to get key list")
	}
	return keys
}

// GetSilent retrieves the configuration regarding the behavior of the servos
// when not in motion. Returns true if silent mode is enabled.
func (g *GRSDevice) GetSilent() bool {
	r := g.sendCommandWithOutput("getsilent")
	silent, err := strconv.ParseBool(r)
	if err != nil {
		log.Fatalf("ERROR: invalid response: %s", r)
	}
	return silent
}

// GetStartupWay retrieves the actual configuration of restrictor orientation
// after power up.
func (g *GRSDevice) GetStartupWay() int {
	cmd := "getstartupway"
	g.sendCommand(cmd)
	r := g.getOutput()
	i, err := strconv.Atoi(r)
	if err != nil {
		log.Fatal("Unable to get Startup Orientation value")
	}
	return i
}

// GetWelcome provides the product name and actual firmware version, so remote
// system can check if connected to the right COM-port.
func (g *GRSDevice) GetWelcome() string {
	g.sendCommand("getwelcome")
	return g.getOutput()
}

// MakePermanent makes all temporary configuration permanent, so that they are
// automatically loaded after each power on.
func (g *GRSDevice) MakePermanent() {
	r := g.sendCommandWithOutput("makepermanent")
	if r != "ok" {
		log.Fatalf("Error making temporary configuration permanent: %s\n", r)
	}
}

// RawCommand sends a raw command to the device.
func (g *GRSDevice) RawCommand(command string) {
	r := g.sendCommandWithOutput(command)
	log.Println(r)
}

// RestoreFactory temporarily reverts to the original factory settings.
// Must be made explicitly permanent with *GRSDevice.MakePermanent() if wanted.
func (g *GRSDevice) RestoreFactory() {
	r := g.sendCommandWithOutput("restorefactory")
	if r != "ok" {
		log.Fatalf("Error restoring factory settings: %s\n", r)
	}
}

// SetColor adjusts the color of a button, depending on the mode.
// When button is used for restrictor control: 4 sets color for 4-way position,
// 8 sets color for 8-way position.
// When button is configured as keybord key, keyboard will set the color for
// that mode
func (g *GRSDevice) SetColor(mode string, red int, green int, blue int) {
	if !isValidMode(mode) {
		log.Fatalf("ERROR: Invalid mode: %s\n", mode)
	}
	if !isValidColor(red) {
		log.Fatalf("ERROR: Invalid value for red: %d\n", red)
	}
	if !isValidColor(green) {
		log.Fatalf("ERROR: Invalid value for green: %d\n", green)
	}
	if !isValidColor(blue) {
		log.Fatalf("ERROR: Invalid value for blue: %d\n", blue)
	}
	cmd := fmt.Sprintf("setcolor,%s,%d,%d,%d")
	r := g.sendCommandWithOutput(cmd)
	if r != "ok" {
		log.Fatalf("ERROR: error setting color: %s\n", r)
	}
}

// SetPosition sets restrictor to position way
//
// Valid values for restrictor are (all, a, b, c, d)
func (g *GRSDevice) SetPosition(restrictor string, way int) {
	validValues := []string{"all", "a", "b", "c", "d"}
	if !funk.Contains(validValues, restrictor) {
		log.Fatalf("ERROR: invalid restrictor value: %s\n", restrictor)
	}
	if !isValidWay(way) {
		log.Fatalf("ERROR: invalid way: %d\n", way)
	}

	log.Printf("Setting restrictor %s position to %d-way", restrictor, way)
	cmd := fmt.Sprintf("setway,%s,%d", restrictor, way)
	g.sendCommand(cmd)

	r := g.getOutput()
	if r != "ok" {
		log.Fatalf("ERROR: \"%q\"\n", r)
	}
}

// SetSilent configures behavior of servos when not in motion. If silent is on,
// the servos are unpowered (low power consumption, low noise but also low
// holding torque).
//
// Recommended setting is false
func (g *GRSDevice) SetSilent(silent bool) {
	s := "off"
	if silent {
		s = "on"
	}
	cmd := fmt.Sprintf("setsilent,%s", s)
	r := g.sendCommandWithOutput(cmd)
	if r != "ok" {
		log.Fatalf("ERROR: error setting silent mode: %s\n", r)
	}
}

// SetStartupWay allows configuration to which position all restrictors will be
// initialized/moved after power up.
func (g *GRSDevice) SetStartupWay(way int) {
	if way != 4 && way != 8 {
		log.Fatalf("ERROR: invalid value %d\n", way)
	}
	cmd := fmt.Sprintf("setstartupway,%d", way)
	r := g.sendCommandWithOutput(cmd)
	if r != "ok" {
		log.Fatalf("ERROR: unable to set startup way: %s\n", r)
	}
	g.MakePermanent()
}

// SetWayForRom sets the way based on rom.
func (g *GRSDevice) SetWayForRom(rom string) {
	log.Printf("Checking ROM: %s", autoRom)

	if funk.Contains(roms, filepath.Base(autoRom)) {
		device.SetPosition(deviceRestrictor, 4)
	} else {
		device.SetPosition(deviceRestrictor, 8)
	}
}

func (g *GRSDevice) Init() {
	c := &serial.Config{Name: devicePath, Baud: 115200}
	d, err := serial.OpenPort(c)
	if err != nil {
		log.Fatal(err)
	}
	g.device = d
}

func findDevice() {
	if devicePath == "auto" {
		ttyDir := "/sys/class/tty"
		files, err := os.ReadDir(ttyDir)
		if err != nil {
			log.Fatal(err)
		}
		for _, file := range files {
			p, _ := filepath.EvalSymlinks(filepath.Join(ttyDir, file.Name()))
			if strings.Contains(p, "usb") {
				const productString = "PRODUCT=2341/8036/100"
				ueventPath := filepath.Join(p, "..", "..", "uevent")
				if _, err := os.Stat(ueventPath); err == nil {
					body, _ := os.ReadFile(ueventPath)
					if strings.Contains(string(body), productString) {
						devicePath = filepath.Join("/dev", file.Name())
						log.Printf("Found tos428: %s\n", devicePath)
					}
				}
			}
		}
	}
}

func initRomList() {
	if romListPath == "" {
		readRomList(romsData)
	} else {
		data, err := os.ReadFile(romListPath)
		if err != nil {
			log.Fatalln(err)
		}
		readRomList(data)
	}
}

func init() {
	flag.StringVar(&autoRom, "rom", "", "auto-detect the way for the specified rom")
	flag.StringVar(&exportFile, "exportromlist", "", "exports the built-in 4-way rom list to specified path")
	flag.StringVar(&romListPath, "romlist", "", "file containing list of 4-way roms. Defaults to built-in list.")
	flag.StringVar(&devicePath, "d", "auto", "path to tos428 device. Set to auto to scan for device. On Windows use COM#")
	flag.StringVar(&deviceRestrictor, "r", "all", "restrictor to apply setting to")
	flag.StringVar(&rawComand, "raw", "", "raw command to send to the device. Used to support features not currently implemented.")
	flag.BoolVar(&getInfo, "info", false, "display device info")
	flag.IntVar(&setWay, "way", 0, "way to set the restrictor (4 or 8)")
	flag.Parse()

	findDevice()
	initRomList()
}

func isValidColor(color int) bool {
	if color >= 0 && color <= 255 {
		return true
	}
	return false
}

func isValidMode(mode string) bool {
	if mode != "4" && mode != "8" && mode != "keyboard" {
		return false
	}
	return true
}

func isValidRestrictor(restrictor string) bool {
	if restrictor == "all" {
		return true
	}
	i, err := strconv.Atoi(restrictor)
	if err != nil {
		return false
	}
	return (i >= 1) && (i <= 4)
}

func isValidWay(way int) bool {
	if way != 4 && way != 8 {
		return false
	}
	return true
}

func readRomList(data []byte) {
	reader := bytes.NewReader(data)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		rom := strings.TrimSpace(scanner.Text())
		if rom != "" {
			roms = append(roms, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Error parsing roms list: %s\n", err)
	}
}

func main() {
	if exportFile != "" {
		err := os.WriteFile(exportFile, romsData, 0644)
		if err != nil {
			log.Fatalf("Error exporting roms list: %s\n", err)
		}
		return
	}

	device := new(GRSDevice)
	device.Init()

	if rawComand != "" {
		device.RawCommand(rawComand)
		return
	}

	if getInfo {
		device.GetInfo()
		return
	}

	if setWay != 0 {
		if !isValidWay(setWay) {
			log.Fatalf("invalid value for -way: %d\n", setWay)
		}
		if !isValidRestrictor(deviceRestrictor) {
			log.Fatalf("invalid value for -r: %s\n", deviceRestrictor)
		}
		device.SetPosition(deviceRestrictor, setWay)
		return
	}

	if autoRom != "" {
		device.SetWayForRom(autoRom)
		return
	}
}
