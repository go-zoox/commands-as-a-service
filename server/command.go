package server

import (
	"os/exec"
	"os/user"
	"syscall"

	"github.com/go-zoox/core-utils/cast"
	"github.com/go-zoox/logger"
)

func setCmdUser(cmd *exec.Cmd, username string) error {
	userX, err := user.Lookup(username)
	if err != nil {
		return err
	}

	logger.Infof("[command] uid=%s gid=%s", userX.Uid, userX.Gid)

	uid := cast.ToInt(userX.Uid)
	gid := cast.ToInt(userX.Gid)

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid: uint32(uid),
		Gid: uint32(gid),
	}

	cmd.Env = append(
		cmd.Env,
		"USER="+username,
		"HOME="+userX.HomeDir,
		"LOGNAME="+username,
		"UID="+userX.Uid,
		"GID="+userX.Gid,
	)

	return nil
}
