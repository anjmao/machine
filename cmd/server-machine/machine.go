package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/docker/machine/commands/mcndirs"
	"github.com/docker/machine/libmachine"
	"github.com/docker/machine/libmachine/auth"
	"github.com/docker/machine/libmachine/crashreport"
	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/engine"
	"github.com/docker/machine/libmachine/host"
	"github.com/docker/machine/libmachine/mcnerror"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/swarm"
)

func main() {
	port := flag.String("port", "8000", "Server port")
	flag.Parse()

	h := &handlers{}
	mux := http.NewServeMux()
	mux.HandleFunc("/create", h.create)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", *port), mux))
}

type handlers struct{}

// create creates docker machine.
func (h *handlers) create(w http.ResponseWriter, r *http.Request) {
	api := libmachine.NewClient(mcndirs.GetBaseDir(), mcndirs.GetMachineCertDir())
	defer api.Close()

	ssh.SetDefaultClient(api.SSHClientType)
	if err := h.cmdCreateInner(api); err != nil {
		log.Fatal(err)
	}

	w.Write([]byte("ok"))
}

type createOptions struct {
	Name             string
	DriverName       string
	StorePath        string
	CaCertPath       string
	CaPrivateKeyPath string
	ClientCertPath   string
	ClientKeyPath    string
	ServerCertPath   string
	ServerKeyPath    string
	ServerCertSANs   []string
}

type DriverOptions interface {
	String(key string) string
	StringSlice(key string) []string
	Int(key string) int
	Bool(key string) bool
}

type driverOpts struct{}

func (driverOpts) String(key string) string        { return "" }
func (driverOpts) StringSlice(key string) []string { return []string{} }
func (driverOpts) Int(key string) int              { return 0 }
func (driverOpts) Bool(key string) bool            { return true }

func (*handlers) cmdCreateInner(api libmachine.API) error {
	opt := &createOptions{
		Name:             "todoname",
		DriverName:       "virtualbox",
		StorePath:        "todo",
		CaCertPath:       "ca.pem",
		CaPrivateKeyPath: "ca-key.pem",
		ClientCertPath:   "cert.pem",
		ClientKeyPath:    "key.pem",
	}

	validName := host.ValidateHostName(opt.Name)
	if !validName {
		return fmt.Errorf("Error creating machine: %s", mcnerror.ErrInvalidHostname)
	}

	// TODO: Fix hacky JSON solution
	rawDriver, err := json.Marshal(&drivers.BaseDriver{
		MachineName: opt.Name,
		StorePath:   opt.StorePath,
	})
	if err != nil {
		return fmt.Errorf("Error attempting to marshal bare driver data: %s", err)
	}

	h, err := api.NewHost(opt.DriverName, rawDriver)
	if err != nil {
		return fmt.Errorf("Error getting new host: %s", err)
	}

	h.HostOptions = &host.Options{
		AuthOptions: &auth.Options{
			CertDir:          mcndirs.GetMachineCertDir(),
			CaCertPath:       opt.CaCertPath,
			CaPrivateKeyPath: opt.CaPrivateKeyPath,
			ClientCertPath:   opt.ClientCertPath,
			ClientKeyPath:    opt.ClientKeyPath,
			ServerCertPath:   opt.ServerCertPath,
			ServerKeyPath:    opt.ServerKeyPath,
			StorePath:        opt.StorePath,
			ServerCertSANs:   opt.ServerCertSANs,
		},
		EngineOptions: &engine.Options{
			ArbitraryFlags:   []string{"TODO"},
			Env:              []string{"TODO"},
			InsecureRegistry: []string{"TODO"},
			Labels:           []string{"TODO"},
			RegistryMirror:   []string{"TODO"},
			StorageDriver:    "TODO",
			TLSVerify:        true,
			InstallURL:       "TODO",
		},
		SwarmOptions: &swarm.Options{
			IsSwarm: false,
		},
	}

	exists, err := api.Exists(h.Name)
	if err != nil {
		return fmt.Errorf("Error checking if host exists: %s", err)
	}
	if exists {
		return mcnerror.ErrHostAlreadyExists{
			Name: h.Name,
		}
	}

	// driverOpts is the actual data we send over the wire to set the
	// driver parameters (an interface fulfilling drivers.DriverOptions,
	// concrete type rpcdriver.RpcFlags).
	driverOpts := &driverOpts{}

	if err := h.Driver.SetConfigFromFlags(driverOpts); err != nil {
		return fmt.Errorf("Error setting machine configuration from flags provided: %s", err)
	}

	if err := api.Create(h); err != nil {
		// Wait for all the logs to reach the client
		time.Sleep(2 * time.Second)

		vBoxLog := ""
		if h.DriverName == "virtualbox" {
			vBoxLog = filepath.Join(api.GetMachinesDir(), h.Name, h.Name, "Logs", "VBox.log")
		}

		return crashreport.CrashError{
			Cause:       err,
			Command:     "Create",
			Context:     "api.performCreate",
			DriverName:  h.DriverName,
			LogFilePath: vBoxLog,
		}
	}

	if err := api.Save(h); err != nil {
		return fmt.Errorf("Error attempting to save store: %s", err)
	}

	log.Printf("To see how to connect your Docker Client to the Docker Engine running on this virtual machine, run: %s env %s", os.Args[0], opt.Name)

	return nil
}

func validateSwarmDiscovery(discovery string) error {
	if discovery == "" {
		return nil
	}

	matched, err := regexp.MatchString(`[^:]*://.*`, discovery)
	if err != nil {
		return err
	}

	if matched {
		return nil
	}

	return fmt.Errorf("Swarm Discovery URL was in the wrong format: %s", discovery)
}
