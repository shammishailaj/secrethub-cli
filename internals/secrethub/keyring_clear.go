package secrethub

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// KeyringClearCommand waits for the keyring item store to expire
// and clears it. If the process receives a kill signal it will
// delete the keyring item and stop.
type KeyringClearCommand struct {
	username string
}

// NewKeyringClearCommand creates a new KeyringClearCommand.
func NewKeyringClearCommand() *KeyringClearCommand {
	return &KeyringClearCommand{}
}

// Register registers the command, arguments and flags on the provided Registerer.
func (cmd *KeyringClearCommand) Register(r Registerer) {
	clause := r.Command("keyring-clear", "Clear the key passphrase from the keyring.").Hidden()
	clause.Arg("username", "The username to clear the passphrase for").Required().StringVar(&cmd.username)

	// Alias for backwards compatibility with old name of command.
	clause.Alias("key-passphrase-clear")

	BindAction(clause, cmd.Run)
}

// Run waits for the keyringItem store to expire and clears it.
// If the process receives a kill signal it will delete the
// keyringItem and stop.
func (cmd *KeyringClearCommand) Run() error {
	keyring := NewKeyring()

	item, err := keyring.Get(cmd.username)
	if err == ErrKeyringItemNotFound {
		// Passphrase already cleared.
		return nil
	} else if err != nil {
		return err
	}

	item.RunningCleanupProcess = true
	err = keyring.Set(cmd.username, item)
	if err != nil {
		return err
	}

	kill := make(chan os.Signal, 1)
	signal.Notify(kill,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGABRT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)

	wait := 0 * time.Second

	for {
		select {
		case <-kill:
			return keyring.Delete(cmd.username)
		case <-time.After(wait):
			item, err := keyring.Get(cmd.username)
			if err == ErrKeyringItemNotFound {
				return nil
			} else if err != nil {
				return err
			}

			if item.IsExpired() {
				err := keyring.Delete(cmd.username)
				if err == nil || err == ErrKeyringItemNotFound {
					return err
				}

				return err
			}

			wait = time.Until(item.ExpiresAt) + 10*time.Millisecond
		}
	}
}
