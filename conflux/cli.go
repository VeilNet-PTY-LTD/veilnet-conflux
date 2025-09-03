package conflux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/veil-net/veilnet"
	"github.com/alecthomas/kong"
)

func login(email string, password string) (string, error) {
	// Prepare login request
	loginReq := LoginRequest{
		Email:    email,
		Password: password,
	}

	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login request: %v", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/auth/v1/token?grant_type=password", "https://supabase.veilnet.org")
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %v", err)
	}

	// Set headers
	req.Header.Set("apikey", "sb_publishable_eNJQSWUp-w9RTIs2V4UDHw_ILjAP_xr")
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make login request: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read login response body: %v", err)
	}

	// Check if request was successful
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var loginResp LoginResponse
	err = json.Unmarshal(body, &loginResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse login response: %v", err)
	}

	return loginResp.AccessToken, nil

}

type CLI struct {
	Version    kong.VersionFlag `short:"v" help:"Print the version and exit"`
	Register   Register         `cmd:"register" help:"Register a new conflux"`
	Unregister UnRegister       `cmd:"unregister" help:"Unregister a conflux"`
	Up         Up               `cmd:"up" help:"Start the conflux"`
}

type Up struct {
	Token    string  `short:"t" help:"The conlfux token, please keep it secret" env:"VEILNET_TOKEN"`
	Portal   bool    `short:"p" help:"Enable portal mode, default: false" default:"false" env:"VEILNET_PORTAL"`
	Guardian string  `short:"g" help:"The Guardian URL (Authentication Server), default: https://guardian.veilnet.org" default:"https://guardian.veilnet.org" env:"VEILNET_GUARDIAN_URL"`
	conflux  Conflux `kong:"-"`
}

func (cmd *Up) Run() error {

	if cmd.Guardian == "" {
		return fmt.Errorf("guardian url is not set")
	}

	if cmd.Token == "" {
		return fmt.Errorf("conflux token is not set")
	}

	cmd.conflux = NewConflux()

	err := cmd.conflux.Start(cmd.Guardian, cmd.Token, cmd.Portal)
	if err != nil {
		return err
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Give the rift time to clean up
	veilnet.Logger.Sugar().Info("Received shutdown signal, shutting down...")

	// Create a channel to signal when cleanup is done
	shutdownComplete := make(chan bool, 1)

	// Stop the conflux
	go func() {
		cmd.conflux.Stop()
		shutdownComplete <- true
	}()

	// Wait for cleanup with timeout
	select {
	case <-shutdownComplete:
		veilnet.Logger.Sugar().Info("Shutdown completed successfully")
	case <-time.After(10 * time.Second):
		veilnet.Logger.Sugar().Warn("Shutdown timeout, forcing exit")
	}

	return nil
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type RegisterRequest struct {
	ConfluxName string `json:"conflux_name"`
	PlaneName   string `json:"plane_name"`
	Tag         string `json:"tag"`
}

type Register struct {
	Email    string `help:"The email to login with VeilNet Guardian"`
	Password string `help:"The password to login with VeilNet Guardian"`
	Name     string `help:"The name of the conflux"`
	Plane    string `help:"The plane to register on"`
	Tag      string `help:"The tag for the conflux"`
}

func (cmd *Register) Run() error {

	accessToken, err := login(cmd.Email, cmd.Password)
	if err != nil {
		return err
	}

	veilnet.Logger.Sugar().Infof("Login successful")

	err = cmd.register(accessToken)
	if err != nil {
		return err
	}
	return nil
}

func (cmd *Register) register(accessToken string) error {

	veilnet.Logger.Sugar().Infof("Registering conflux %s on plane %s with tag %s", cmd.Name, cmd.Plane, cmd.Tag)

	url := fmt.Sprintf("%s/conflux?conflux_name=%s&plane_name=%s&tag=%s", "https://guardian.veilnet.org", cmd.Name, cmd.Plane, cmd.Tag)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create register request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make register request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read register response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("register failed with status %d: %s", resp.StatusCode, string(body))
	}

	veilnet.Logger.Sugar().Infof("Conflux registered successfully! Token: %s", string(body))

	return nil
}

type UnRegister struct {
	Email    string `help:"The email to login with VeilNet Guardian"`
	Password string `help:"The password to login with VeilNet Guardian"`
	Name     string `help:"The name of the conflux"`
	Plane    string `help:"The plane to register on"`
}

func (cmd *UnRegister) Run() error {

	accessToken, err := login(cmd.Email, cmd.Password)
	if err != nil {
		return err
	}

	veilnet.Logger.Sugar().Infof("Login successful")

	err = cmd.unregister(accessToken)
	if err != nil {
		return err
	}
	return nil
}

func (cmd *UnRegister) unregister(accessToken string) error {

	veilnet.Logger.Sugar().Infof("Unregistering conflux %s on plane %s", cmd.Name, cmd.Plane)

	url := fmt.Sprintf("%s/conflux?conflux_name=%s&plane_name=%s", "https://guardian.veilnet.org", cmd.Name, cmd.Plane)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create register request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make register request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read register response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("register failed with status %d: %s", resp.StatusCode, string(body))
	}

	veilnet.Logger.Sugar().Infof("Conflux unregistered successfully!")

	return nil
}
