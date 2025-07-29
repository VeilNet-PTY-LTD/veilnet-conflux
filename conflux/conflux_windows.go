//go:build windows
// +build windows

package conflux

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	veilnet "github.com/VeilNet-PTY-LTD/veilnet"
	"golang.org/x/sys/windows"
	tun "golang.zx2c4.com/wireguard/tun"
)

//go:embed wintun.dll
var wintunDLL []byte

type conflux struct {
	anchor           *veilnet.Anchor
	device           tun.Device
	portal           bool
	gateway          string
	iface            string
	bypassRoutes     sync.Map
	ipForwardEnabled bool

	once   sync.Once
	ctx    context.Context
	cancel context.CancelFunc
}

func newConflux() *conflux {
	ctx, cancel := context.WithCancel(context.Background())
	anchor := veilnet.NewAnchor()
	conflux := &conflux{
		anchor: anchor,
		ctx:    ctx,
		cancel: cancel,
	}
	return conflux
}

func (c *conflux) Start(apiBaseURL, anchorToken string, portal bool) error {

	// Set portal
	if portal {
		return fmt.Errorf("portal is not supported on Windows")
	}

	// Get the default gateway and interface
	err := c.DetectHostGateway()
	if err != nil {
		return err
	}

	// Set bypass routes
	c.AddBypassRoutes()

	// Create the TUN device
	err = c.CreateTUN()
	if err != nil {
		return err
	}

	// Start the anchor
	err = c.StartAnchor(apiBaseURL, anchorToken, portal)
	if err != nil {
		return err
	}

	// Get the IP address
	cidr, err := c.anchor.GetCIDR()
	if err != nil {
		return err
	}
	ipAddr, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}
	ip := ipAddr.String()
	netmask := fmt.Sprintf("%d.%d.%d.%d", ipNet.Mask[0], ipNet.Mask[1], ipNet.Mask[2], ipNet.Mask[3])

	// Configure the host
	err = c.ConfigHost(ip, netmask)
	if err != nil {
		return err
	}

	// Start the ingress and egress threads
	go c.ingress()
	go c.egress()

	return nil
}

func (c *conflux) Stop() {
	c.once.Do(func() {
		c.cancel()
		if c.anchor != nil {
			c.anchor.Stop()
		}
		c.CleanHostConfiguraions()
		c.RemoveBypassRoutes()
		if c.device != nil {
			c.device.Close()
		}
	})
}

func (c *conflux) StartAnchor(apiBaseURL, anchorToken string, portal bool) error {

	// Start the anchor
	err := c.anchor.Start(apiBaseURL, anchorToken, portal)
	if err != nil {
		return err
	}

	return nil
}

func (c *conflux) StopAnchor() {
	c.anchor.Stop()
}

func (c *conflux) IsAnchorAlive() bool {
	return c.anchor.IsAlive()
}

func (c *conflux) CreateTUN() error {
	// Extract the wintun.dll to the current directory
	executablePath, err := os.Executable()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(executablePath)
	dllPath := filepath.Join(exeDir, "wintun.dll")

	// Check if the file already exists
	if _, err := os.Stat(dllPath); os.IsNotExist(err) {
		// File does not exist, so write it
		if err := os.WriteFile(dllPath, wintunDLL, 0644); err != nil {
			return err
		}
	} else if err != nil {
		// An error occurred while checking the file
		return err
	}

	// Set the GUID for the TUN device
	tun.WintunStaticRequestedGUID = &windows.GUID{
		Data1: 0x564E4554,                                              // "VNET" in ASCII
		Data2: 0x564E,                                                  // "VN" in ASCII
		Data3: 0x4554,                                                  // "ET" in ASCII
		Data4: [8]byte{0x56, 0x45, 0x49, 0x4C, 0x4E, 0x45, 0x54, 0x00}, // "VEILNET" in ASCII
	}

	// Create a new TUN device
	tun, err := tun.CreateTUN("veilnet", 1500)
	if err != nil {
		return err
	}
	c.device = tun
	return nil
}

func (c *conflux) CloseTUN() error {
	if c.device != nil {
		err := c.device.Close()
		if err != nil {
			veilnet.Logger.Sugar().Errorf("failed to close TUN device: %v", err)
			return err
		}
	}
	return nil
}

func (c *conflux) DetectHostGateway() error {

	// Get the host default gateway and interface
	cmd := exec.Command("route", "print", "0.0.0.0")
	out, err := cmd.Output()
	if err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to get host default gateway: %v", err)
		return err
	}

	// Parse the output
	lines := strings.Split(string(out), "\n")
	var gateway string
	var iface string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			gateway = fields[2]
			iface = fields[3]
			break
		}
	}

	// If the host default gateway or interface is not found, return an error
	if gateway == "" || iface == "" {
		veilnet.Logger.Sugar().Errorf("Host default gateway or interface not found")
		return fmt.Errorf("host default gateway or interface not found")
	}

	// Store the host default gateway and interface
	veilnet.Logger.Sugar().Infof("Found Host Default gateway: %s via interface %s", gateway, iface)
	c.gateway = gateway
	c.iface = iface
	return nil
}

func (c *conflux) AddBypassRoutes() {
	hosts := []string{"stun.cloudflare.com", "turn.cloudflare.com", "guardian.veilnet.org"}

	for _, host := range hosts {
		// Resolve IP addresses
		ips, err := net.LookupIP(host)
		if err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to resolve %s: %v", host, err)
			continue
		}

		for _, ip := range ips {
			// Add route for IPv4 addresses
			if ip4 := ip.To4(); ip4 != nil {
				dest := ip4.String()
				cmd := exec.Command("route", "add", dest, "mask", "255.255.255.255", c.gateway)
				cmd.Run()
				// Store the bypass route
				c.bypassRoutes.Store(host, dest)
			}
		}
	}
}

func (c *conflux) RemoveBypassRoutes() {
	c.bypassRoutes.Range(func(key, value interface{}) bool {
		// Remove bypass route
		cmd := exec.Command("route", "delete", value.(string), "mask", "255.255.255.255", c.gateway)
		err := cmd.Run()
		if err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to clear bypass route for %s: %v", key, err)
			return false
		}
		return true
	})
}

func (c *conflux) Read(bufs [][]byte, batchSize int) (int, error) {
	return c.anchor.Read(bufs, batchSize)
}

func (c *conflux) Write(bufs [][]byte, sizes []int) (int, error) {
	return c.anchor.Write(bufs, sizes)
}

func (c *conflux) ingress() {
	bufs := make([][]byte, c.device.BatchSize())
	for {
		select {
		case <-c.anchor.Ctx.Done():
			veilnet.Logger.Sugar().Info("Portal ingress stopped")
			return
		default:
			n, err := c.anchor.Read(bufs, c.device.BatchSize())
			if err != nil {
				veilnet.Logger.Sugar().Errorf("failed to read from anchor: %v", err)
				continue
			}
			for i := 0; i < n; i++ {
				newBuf := make([]byte, 16+len(bufs[i]))
				copy(newBuf[16:], bufs[i])
				bufs[i] = newBuf
			}
			c.device.Write(bufs[:n], 16)
		}
	}
}

func (c *conflux) egress() {
	bufs := make([][]byte, c.device.BatchSize())
	sizes := make([]int, c.device.BatchSize())
	mtu, err := c.device.MTU()
	if err != nil {
		veilnet.Logger.Sugar().Errorf("failed to get TUN MTU: %v", err)
		// Use default MTU if we can't get the actual one
		mtu = 1500
	}
	// Pre-allocate buffers
	for i := range bufs {
		bufs[i] = make([]byte, mtu)
	}

	for {
		select {
		case <-c.anchor.Ctx.Done():
			veilnet.Logger.Sugar().Info("Portal egress stopped")
			return
		default:
			n, err := c.device.Read(bufs, sizes, 0)
			if err != nil {
				veilnet.Logger.Sugar().Errorf("failed to read from TUN device: %v", err)
				continue
			}
			c.Write(bufs[:n], sizes[:n])
		}
	}
}

// ConfigHost configures the TUN interface with the given IP address and netmask
// It also sets up iptables FORWARD rules and NAT for the TUN interface
// It also enables IP forwarding if it is not already enabled
func (c *conflux) ConfigHost(ip, netmask string) error {

	// Add bypass routes for Veil Master
	veilHost := c.anchor.GetVeilHost()
	if veilHost != "" {
		cmd := exec.Command("route", "add", veilHost, "mask", "255.255.255.255", c.gateway)
		err := cmd.Run()
		if err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to add route for Veil Master at %s via %s: %v", veilHost, c.gateway, err)
		} else {
			veilnet.Logger.Sugar().Infof("Added route to Veil Master at %s via %s", veilHost, c.gateway)
		}
	}

	// Set the IP address and netmask
	cmd := exec.Command("netsh", "interface", "ip", "set", "address", "name=veilnet", "static", ip, netmask)
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to configure VeilNet TUN IP address: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Set VeilNet TUN to %s", ip)

	// Set the DNS server
	cmd = exec.Command("netsh", "interface", "ip", "set", "dns", "name=veilnet", "static", "1.1.1.1")
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to configure VeilNet TUN DNS: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Set VeilNet TUN DNS to 1.1.1.1")

	// Get the interface index
	iface, err := net.InterfaceByName("veilnet")
	if err != nil {
		veilnet.Logger.Sugar().Errorf("failed to get VeilNet TUN interface index: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Got VeilNet TUN interface index: %d", iface.Index)

	// Set the route
	cmd = exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0", ip, "metric", "5", "if", strconv.Itoa(iface.Index))
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to set VeilNet TUN as alternate gateway: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Set VeilNet TUN as preferred gateway")

	return nil
}

// CleanHostConfiguraions removes the iptables FORWARD rules and NAT rule for the TUN interface
// It also disables IP forwarding if it was not enabled
func (c *conflux) CleanHostConfiguraions() {

	// Get the interface index
	iface, err := net.InterfaceByName("veilnet")
	if err != nil {
		veilnet.Logger.Sugar().Errorf("failed to get VeilNet TUN interface index: %v", err)
		return
	}

	// Remove the route
	cmd := exec.Command("route", "delete", "0.0.0.0", "mask", "0.0.0.0", "if", strconv.Itoa(iface.Index))
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to remove VeilNet TUN route: %v", err)
	}
	veilnet.Logger.Sugar().Infof("Removed VeilNet TUN as preferred gateway")

	// Remove the bypass routes for Veil Master
	veilHost := c.anchor.GetVeilHost()
	if veilHost != "" {
		cmd := exec.Command("route", "delete", veilHost, "mask", "255.255.255.255", c.gateway)
		err := cmd.Run()
		if err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to remove route for Veil Master at %s via %s: %v", veilHost, c.gateway, err)
		}
	}
	veilnet.Logger.Sugar().Infof("Removed bypass routes")
}
