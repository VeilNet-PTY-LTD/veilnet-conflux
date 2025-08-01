//go:build linux
// +build linux

package conflux

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	veilnet "github.com/VeilNet-PTY-LTD/veilnet"
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
	c.portal = portal

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
		os.Exit(1)
	}()

	return nil
}

func (c *conflux) Stop() {
	c.once.Do(func() {
		if c.anchor != nil {
			c.anchor.Stop()
		}
		c.anchor = nil
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

	// Get the host default gateway and interface
	cmd := exec.Command("ip", "route", "show", "default")
	out, err := cmd.Output()
	if err != nil {
		veilnet.Logger.Sugar().Errorf("Failed to get default route: %v", err)
		return err
	}
	lines := strings.Split(string(out), "\n")
	var gateway, iface string
	for _, line := range lines {
		if strings.HasPrefix(line, "default") {
			fields := strings.Fields(line)
			for i := 0; i < len(fields); i++ {
				if fields[i] == "via" && i+1 < len(fields) {
					gateway = fields[i+1]
				}
				if fields[i] == "dev" && i+1 < len(fields) {
					iface = fields[i+1]
				}
			}
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
				cmd := exec.Command("ip", "route", "add", dest, "via", c.gateway, "dev", c.iface)
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
		cmd := exec.Command("ip", "route", "del", value.(string))
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

	// Add bypass route for Veil Master
	veilHost := c.anchor.GetVeilHost()
	if veilHost != "" {
		cmd := exec.Command("ip", "route", "add", veilHost, "via", c.gateway, "dev", c.iface)
		cmd.Run()
	}

	// Flush existing IPs first
	cmd := exec.Command("ip", "addr", "flush", "dev", "veilnet")
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to clear existing IPs: %v", err)
		return err
	}

	// Set the IP address
	cmd = exec.Command("ip", "addr", "add", fmt.Sprintf("%s/%s", ip, netmask), "dev", "veilnet")
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to set IP address: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("VeilNet TUN IP address set to %s", ip)

	// Set the interface up
	cmd = exec.Command("ip", "link", "set", "up", "veilnet")
	if err := cmd.Run(); err != nil {
		veilnet.Logger.Sugar().Errorf("failed to set interface up: %v", err)
		return err
	}
	veilnet.Logger.Sugar().Infof("VeilNet TUN interface set to up")

	if c.portal {

		// Set iptables FORWARD
		cmd = exec.Command("iptables", "-A", "FORWARD", "-i", "veilnet", "-j", "ACCEPT")
		if err := cmd.Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("failed to set inbound iptables FORWARD rules: %v", err)
			return err
		}
		cmd = exec.Command("iptables", "-A", "FORWARD", "-o", "veilnet", "-j", "ACCEPT")
		if err := cmd.Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("failed to set outbound iptables FORWARD rules: %v", err)
			return err
		}
		veilnet.Logger.Sugar().Infof("Updated iptables FORWARD rules for VeilNet TUN")

		// Set up NAT
		cmd = exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", c.iface, "-j", "MASQUERADE")
		if err := cmd.Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("failed to set NAT rules: %v", err)
			return err
		}
		veilnet.Logger.Sugar().Infof("Set up NAT for VeilNet TUN")

		// Check if IP forwarding is already enabled
		cmd = exec.Command("sysctl", "-n", "net.ipv4.ip_forward")
		output, err := cmd.Output()
		if err != nil {
			veilnet.Logger.Sugar().Errorf("failed to check IP forwarding status: %v", err)
			return err
		}

		// Trim whitespace and check if it's enabled
		c.ipForwardEnabled = strings.TrimSpace(string(output)) == "1"

		if !c.ipForwardEnabled {
			// Enable IP forwarding
			cmd = exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1")
			if err := cmd.Run(); err != nil {
				veilnet.Logger.Sugar().Errorf("failed to enable IP forwarding: %v", err)
				return err
			}
			veilnet.Logger.Sugar().Infof("IP forwarding enabled")
		} else {
			veilnet.Logger.Sugar().Infof("IP forwarding already enabled")
		}
	} else {
		// Delete the default route
		if err := exec.Command("ip", "route", "del", "default", "via", c.gateway, "dev", c.iface).Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to delete default route: %v", err)
			return err
		}

		// Add the default route with high metric
		if err := exec.Command("ip", "route", "add", "default", "via", c.gateway, "dev", c.iface, "metric", "50").Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to add default route: %v", err)
			return err
		}
		veilnet.Logger.Sugar().Infof("Altered host default route via %s on %s with metric 50", c.gateway, c.iface)

		// Set the TUN interface as the default route
		if err := exec.Command("ip", "route", "add", "default", "dev", "veilnet").Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to set default route: %v", err)
			return err
		}
		veilnet.Logger.Sugar().Infof("Set veilnet as default route")
	}

	return nil
}

// CleanHostConfiguraions removes the iptables FORWARD rules and NAT rule for the TUN interface
// It also disables IP forwarding if it was not enabled
func (c *conflux) CleanHostConfiguraions() {

	// Remove the route to the Veil Master
	veilHost := c.anchor.GetVeilHost()
	if veilHost != "" {
		cmd := exec.Command("ip", "route", "del", veilHost, "via", c.gateway, "dev", c.iface)
		cmd.Run()
	}

	if c.portal {

		// Remove iptables FORWARD rules
		cmd := exec.Command("iptables", "-D", "FORWARD", "-i", "veilnet", "-j", "ACCEPT")
		if err := cmd.Run(); err != nil {
			veilnet.Logger.Sugar().Warnf("failed to remove inbound iptables FORWARD rule: %v", err)
		}
		cmd = exec.Command("iptables", "-D", "FORWARD", "-o", "veilnet", "-j", "ACCEPT")
		if err := cmd.Run(); err != nil {
			veilnet.Logger.Sugar().Warnf("failed to remove outbound iptables FORWARD rule: %v", err)
		}
		veilnet.Logger.Sugar().Infof("Removed inbound and outbound iptables FORWARD rules")

		// Remove NAT rule
		cmd = exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", c.iface, "-j", "MASQUERADE")
		if err := cmd.Run(); err != nil {
			veilnet.Logger.Sugar().Warnf("failed to remove NAT rule: %v", err)
		}
		veilnet.Logger.Sugar().Infof("Removed NAT rule")

		// Disable IP forwarding if it was not enabled
		if !c.ipForwardEnabled {
			cmd = exec.Command("sysctl", "-w", "net.ipv4.ip_forward=0")
			if err := cmd.Run(); err != nil {
				veilnet.Logger.Sugar().Warnf("failed to disable IP forwarding: %v", err)
			}
			veilnet.Logger.Sugar().Infof("Disabled IP forwarding")
		}
	} else {
		// Remove veilnet TUN as default route
		if err := exec.Command("ip", "route", "del", "default", "dev", "veilnet").Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to remove veilnet TUN as default route: %v", err)
		}
		veilnet.Logger.Sugar().Infof("Removed veilnet TUN as default route")

		// Delete the altered host default route
		if err := exec.Command("ip", "route", "del", "default", "via", c.gateway, "dev", c.iface).Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to delete altered host default route: %v", err)
		}
		veilnet.Logger.Sugar().Infof("Removed altered host default route")

		// Restore the host default route
		if err := exec.Command("ip", "route", "add", "default", "via", c.gateway, "dev", c.iface).Run(); err != nil {
			veilnet.Logger.Sugar().Errorf("Failed to restore default route on host: %v", err)
		}
		veilnet.Logger.Sugar().Infof("Restored default route on host")
	}
}
