package git

import (
	"log"
	"os"
	"path"
	"testing"

	"github.com/portainer/portainer/api/filesystem"
	"github.com/stretchr/testify/assert"
)

func TestCloneRepository(t *testing.T) {
	service := NewService()

	destination := path.Join(os.TempDir(), "git-test")
	teardown(destination)

	err := service.CloneRepository(destination, "https://github.com/portainer/portainer-compose", "HEAD", false, "", "")
	assert.NoError(t, err)

	// check if file exists
	if exists, _ := filesystem.FileExists(destination); !exists {
		t.Error("Expect git path to exist")
	}

	teardown(destination)
}

func teardown(clonePath string) {
	err := os.RemoveAll(clonePath)
	if err != nil {
		log.Fatalln(err)
	}

}
