package app

import (
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/daemon"
)

// agentSkillDoctorCheck reports the skill's state without turning doctor
// yellow for people who never opted in: a Claude Code that simply has no
// plugin yet is information with the remedy attached, which agents read from
// the JSON check. Only drift warns — a universal skills directory exists, so
// some agent reads it, and Orbit's copy there is not current. The check never
// fails doctor.
func agentSkillDoctorCheck() daemon.DoctorCheck {
	check := daemon.DoctorCheck{Name: agentSkillCheckName}
	routes, err := newSkillRoutes()
	if err != nil {
		check.Status, check.Message = daemon.CheckInfo, err.Error()
		return check
	}
	claudeCode, runnable := routes.claudeCode()
	universal := pathExists(filepath.Join(routes.home, ".agents", "skills"))
	if !claudeCode && !universal {
		check.Status, check.Message = daemon.CheckInfo, "no coding agent detected"
		return check
	}
	var attention, pending, context []string
	state, err := routes.dirState()
	switch {
	case err != nil:
		attention = append(attention, err.Error())
	case state == skillDirCurrent:
		context = append(context, routes.dir()+" is current")
	case universal:
		attention = append(attention, routes.dir()+" is "+state)
	default:
		// Nothing on this machine reads the universal directory yet.
		context = append(context, routes.dir()+" is "+state)
	}
	if claudeCode && routes.pluginRouteAvailable() {
		switch installed, known := claudePluginRecorded(routes.claudeConfigDir(), routes.pluginID); {
		case !known:
			context = append(context, "Claude Code plugin state unknown")
		case !installed && !runnable:
			pending = append(pending, "Claude Code plugin not installed ('claude' is not on PATH in this shell)")
		case !installed:
			pending = append(pending, "Claude Code plugin not installed")
		default:
			context = append(context, "Claude Code plugin installed")
		}
	}
	check.Message = strings.Join(append(append(attention, pending...), context...), "; ")
	switch {
	case len(attention) > 0:
		check.Status, check.Hint = daemon.CheckWarn, "run: orbit skill install --yes"
	case len(pending) > 0:
		check.Status, check.Hint = daemon.CheckInfo, "run: orbit skill install --yes"
	default:
		check.Status = daemon.CheckPass
	}
	return check
}
