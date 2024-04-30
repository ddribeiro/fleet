package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fleetdm/fleet/v4/orbit/pkg/scripts"
	"github.com/fleetdm/fleet/v4/server/contexts/ctxerr"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/osquery/osquery-go"
	osquery_gen "github.com/osquery/osquery-go/gen/osquery"
)

type QueryResponse = osquery_gen.ExtensionResponse

// Client defines the methods required for the API requests to the server. The
// fleet.OrbitClient type satisfies this interface.
type Client interface {
	GetHostScript(execID string) (*fleet.HostScriptResult, error)
	GetInstaller(installerID, downloadDir string) (string, error)
	SaveHostScriptResult(result *fleet.HostScriptResultPayload) error
}

type QueryClient interface {
	Query(context.Context, string) (*QueryResponse, error)
}

type Runner struct {
	OsqueryClient QueryClient
	OrbitClient   Client

	// tempDirFn is the function to call to get the temporary directory to use,
	// inside of which the script-specific subdirectories will be created. If nil,
	// the user's temp dir will be used (can be set to t.TempDir in tests).
	tempDirFn func() string

	// execCmdFn can be set for tests to mock actual execution of the script. If
	// nil, execCmd will be used, which has a different implementation on Windows
	// and non-Windows platforms.
	execCmdFn func(ctx context.Context, scriptPath string) ([]byte, int, error)

	// can be set for tests to replace os.RemoveAll, which is called to remove
	// the script's temporary directory after execution.
	removeAllFn func(string) error
}

func NewRunner(client Client, socketPath string, timeout time.Duration) (*Runner, error) {
	r := &Runner{
		OrbitClient: client,
	}

	osqueryClient, err := osquery.NewClient(socketPath, timeout)
	if err != nil {
		return nil, fmt.Errorf("creating new osquery client: %w", err)
	}

	r.OsqueryClient = osqueryClient.Client

	return r, nil
}

func (r *Runner) InstallSoftware(ctx context.Context, installer *fleet.OrbitSoftwareInstaller) error {
	shouldInstall, err := r.preConditionCheck(ctx, installer.PreInstallCondition)
	if err != nil {
		return err
	}

	if !shouldInstall {
		return nil
	}

	installScript, err := r.OrbitClient.GetHostScript(installer.InstallScript)
	if err != nil {
		return err
	}

	postInstallScript, err := r.OrbitClient.GetHostScript(installer.PostInstallScript)
	if err != nil {
		return err
	}

	if r.tempDirFn == nil {
		r.tempDirFn = os.TempDir
	}
	tmpDir := r.tempDirFn()

	installerPath, err := r.OrbitClient.GetInstaller(installer.SoftwareId, tmpDir)
	if err != nil {
		return err
	}

	// remove tmp directory and installer
	defer func() {
		if r.removeAllFn == nil {
			r.removeAllFn = os.RemoveAll
		}
		r.removeAllFn(tmpDir)
	}()

	err = r.runInstallerScript(ctx, installScript, installerPath)
	if err != nil {
		return err
	}

	err = r.runInstallerScript(ctx, postInstallScript, installerPath)
	if err != nil {
		return err
	}

	return nil
}

func (r *Runner) preConditionCheck(ctx context.Context, query string) (bool, error) {
	res, err := r.OsqueryClient.Query(ctx, query)
	if err != nil {
		return false, ctxerr.Wrap(ctx, err, "precondition check")
	}

	if res.Status.Code != 0 {
		return false, ctxerr.Wrap(ctx, fmt.Errorf("non-zero query status: %d \"%s\"", res.Status.Code, res.Status.Message))
	}

	if len(res.Response) > 0 {
		return true, nil
	}

	return false, nil
}

func (r *Runner) runInstallerScript(ctx context.Context, script *fleet.HostScriptResult, installerPath string) error {
	// run script in installer directory
	scriptPath := filepath.Join(installerPath, strconv.Itoa(int(script.ID)))
	if err := os.WriteFile(scriptPath, []byte(script.ScriptContents), os.ModePerm); err != nil {
		return ctxerr.Wrap(ctx, err, "writing script")
	}

	print("installer path: ", installerPath+"\n")
	print("script path: ", scriptPath+"\n")

	if r.execCmdFn == nil {
		r.execCmdFn = scripts.ExecCmd
	}

	start := time.Now()
	output, exitCode, err := r.execCmdFn(ctx, scriptPath)
	duration := time.Since(start)

	if err = r.OrbitClient.SaveHostScriptResult(&fleet.HostScriptResultPayload{
		ExecutionID: script.ExecutionID,
		Output:      string(output),
		Runtime:     int(duration.Seconds()),
		ExitCode:    exitCode,
	}); err != nil {
		return fmt.Errorf("save script result: %w", err)
	}

	return nil
}
