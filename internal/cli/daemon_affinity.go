package cli

import "github.com/spf13/cobra"

type DaemonAffinity string

const (
	DaemonIndependent              DaemonAffinity = "independent"
	DaemonMatchingConfig           DaemonAffinity = "matching-config"
	DaemonReconciledConfig         DaemonAffinity = "reconciled-config"
	DaemonMatchingReconciledConfig DaemonAffinity = "matching-reconciled-config"
)

const daemonAffinityAnnotation = "orbit.daemon-affinity"

func WithDaemonAffinity(cmd *cobra.Command, affinity DaemonAffinity) *cobra.Command {
	switch affinity {
	case DaemonIndependent, DaemonMatchingConfig, DaemonReconciledConfig, DaemonMatchingReconciledConfig:
	default:
		panic("invalid daemon affinity: " + string(affinity))
	}
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[daemonAffinityAnnotation] = string(affinity)
	return cmd
}

// Only the command's own declaration is returned. The app owns command-tree
// scope so this metadata cannot implicitly change other admission policies.
func DeclaredDaemonAffinity(cmd *cobra.Command) (DaemonAffinity, bool) {
	affinity := DaemonAffinity(cmd.Annotations[daemonAffinityAnnotation])
	return affinity, affinity != ""
}
