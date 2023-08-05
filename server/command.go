package server

import (
	"os/exec"
	"os/user"
	"syscall"

	"github.com/go-zoox/core-utils/cast"
)

func setCmdUser(cmd *exec.Cmd, username string) error {
	userX, err := user.Lookup(username)
	if err != nil {
		return err
	}

	uid := cast.ToInt(userX.Uid)
	gid := cast.ToInt(userX.Gid)

	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid: uint32(uid),
		Gid: uint32(gid),
	}

	return nil
}
