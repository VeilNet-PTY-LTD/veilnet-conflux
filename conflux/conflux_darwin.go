//go:build darwin
// +build darwin

package conflux

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/veil-net/veilnet"
	tun "golang.zx2c4.com/wireguard/tun"
)

type conflux struct {
	anchor           *veilnet.Anchor
	device           tun.Device
	portal           bool
	gateway          string
	iface            string
	bypassRoutes     sync.Map
	ipForwardEnabled bool

	once sync.Once
}

func newConflux() *conflux {
	return &conflux{}
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

	// Create the anchor
	c.anchor = veilnet.NewAnchor()

	// Start the anchor
	err = c.StartAnchor(apiBaseURL, anchorToken, portal)
	if err != nil {
		return err
	}

	// Get the CIDR
	cidr, err := c.anchor.GetCIDR()
	if err != nil {
		return err
	}

	// Split CIDR into IP and netmask
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid CIDR format: %s", cidr)
	}
	ip := parts[0]
	netmask := parts[1]

	// Configure the host
	err = c.ConfigHost(ip, netmask)
	if err != nil {
		return err
	}

	// Start the ingress and egress threads
	go c.ingress()
	go c.egress()

	// Check if the anchor is alive and if not, stop the conflux and exit
	go func() {
		<-c.anchor.Ctx.Done()
		veilnet.Logger.Sugar().Info("Anchor stopped")
		c.Stop()
		os.Exit(1)
	}()

	return nil
}

func (c *conflux) Stop() {
	c.once.Do(func() {
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

func (c *conflux) CreateTUN() error {
	var err error
	c.device, err = tun.CreateTUN("veilnet", 1500)
	if err != nil {
		veilnet.Logger.Sugar().Errorf("failed to create TUN device: %v", err)
		return err
	}
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

	cmd := exec.Command("route", "-n", "get", "default")
	out, err := cmd.Output()
	if err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to get default route: %v", err)
		return err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gateway:") {
			c.gateway = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
		if strings.HasPrefix(line, "interface:") {
			c.iface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}

	if c.gateway == "" || c.iface == "" {
		err = fmt.Errorf("default gateway or interface not found")
		veilnet.Logger.Sugar().Errorf("Host default gateway or interface not found")
		return err
	}

	veilnet.Logger.Sugar().Infof("Found Host Default gateway: %s via interface %s", c.gateway, c.iface)
	return nil
}

func (c *conflux) AddBypassRoutes() {
	hosts := []string{"stun.cloudflare.com", "turn.cloudflare.com", "guardian.veilnet.org", "turn.veilnet.org"}

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
				cmd := exec.Command("route", "-n", "add", dest, c.gateway, "-interface", c.iface)
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
		cmd := exec.Command("route", "-n", "del", value.(string))
		err := cmd.Run()
		if err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to clear bypass route for %s: %v", key, err)
			return false
		}
		return true
	})
}

func (c *conflux) Read(bufs [][]byte, batchSize int) int {
	return c.anchor.Read(bufs, batchSize)
}

func (c *conflux) Write(bufs [][]byte, sizes []int) int {
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
			n := c.Read(bufs, c.device.BatchSize())
			for i := 0; i < n; i++ {
				newBuf := make([]byte, 16+len(bufs[i]))
				copy(newBuf[16:], bufs[i])
				bufs[i] = newBuf
			}
			if n > 0 {
				c.device.Write(bufs[:n], 16)
			}
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
				continue
			}
			if n > 0 {
				c.Write(bufs[:n], sizes[:n])
			}
		}
	}
}

// ConfigHost configures the TUN interface with the given IP address and netmask
// It also sets up iptables FORWARD rules and NAT for the TUN interface
// It also enables IP forwarding if it is not already enabled
func (c *conflux) ConfigHost(ip, netmask string) error {

	// Add bypass route for Veil Master
	veilHost := c.anchor.GetVeilHost()
	if veilHost != "" {
		cmd := exec.Command("route", "-n", "add", veilHost, c.gateway, "-interface", c.iface)
		cmd.Run()
	}
	// Bring the interface up
	if err := exec.Command("ifconfig", "veilnet", "up").Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to bring interface veilnet up: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Set VeilNet TUN interface up")

	// Set the IP address and netmask
	if err := exec.Command("ifconfig", "veilnet", "inet", ip, "netmask", c.convertNetmask(netmask)).Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to set IP %s/%s on veilnet: %v", ip, netmask, err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Set VeilNet TUN IP to %s/%s", ip, netmask)

	// Delete the original default route
	if err := exec.Command("route", "-n", "delete", "default").Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to delete original default route: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Deleted original default route")

	// Recreate the original default route with higher hopcount (lower priority)
	if err := exec.Command("route", "-n", "add", "default", c.gateway, "-hopcount", "10").Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to recreate default route with higher hopcount: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Recreated default route with hopcount 10")

	// Add a route through the TUN interface with lower hopcount (higher priority)
	if err := exec.Command("route", "-n", "add", "default", "-interface", "veilnet", "-hopcount", "5").Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to set default route: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("Set veilnet as default route with hopcount 5")

	return nil
}

// convertNetmask converts CIDR notation to dotted decimal notation
func (c *conflux) convertNetmask(cidr string) string {
	switch cidr {
	case "8":
		return "255.0.0.0"
	case "16":
		return "255.255.0.0"
	case "24":
		return "255.255.255.0"
	case "32":
		return "255.255.255.255"
	default:
		return "255.255.255.0" // Default to /24
	}
}

// CleanHostConfiguraions removes the iptables FORWARD rules and NAT rule for the TUN interface
// It also disables IP forwarding if it was not enabled
func (c *conflux) CleanHostConfiguraions() {

	// Remove the route to the Veil Master
	veilHost := c.anchor.GetVeilHost()
	if veilHost != "" {
		cmd := exec.Command("route", "-n", "del", veilHost, c.gateway)
		cmd.Run()
	}

	// Delete the route through the TUN interface
	if err := exec.Command("route", "-n", "delete", "default", "-interface", "veilnet").Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to delete TUN default route: %v", err)
	}
	veilnet.Logger.Sugar().Infof("Deleted TUN default route")

	// Delete the altered default route
	if err := exec.Command("route", "-n", "delete", "default").Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to delete altered default route: %v", err)
	}
	veilnet.Logger.Sugar().Infof("Deleted altered default route")

	// Restore the original host default route
	if err := exec.Command("route", "-n", "add", "default", c.gateway).Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to restore host default route: %v", err)
	}
	veilnet.Logger.Sugar().Infof("Restored host default route")
}
