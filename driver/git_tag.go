package driver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/blang/semver"
)

type GitTagDriver struct {
	Prefix     string
	URI        string
	Repository string
	Branch     string
}

const nothingToDescribe = "No names found, cannot describe anything"
const notAValidObject = "Not a valid object name HEAD"

func currentVersion(prefix string, describeOutput string) (semver.Version, error) {
	tags := strings.Split(string(describeOutput), "\n")

	max := semver.Version{}

	for _, tag := range tags {
		versionStr := strings.TrimSpace(tag)
		if len(versionStr) == 0 {
			continue
		}

		if !strings.HasPrefix(versionStr, prefix) {
			return semver.Version{}, errors.New("Unexpected tag: " + tag)
		}

		versionStr = versionStr[len(prefix):]
		current, err := semver.Parse(versionStr)
		if err != nil {
			return semver.Version{}, err
		}

		if current.Compare(max) > 0 {
			max = current
		}
	}

	return max, nil
}

func (driver *GitTagDriver) readVersion() (semver.Version, bool, error) {
	gitFetch := exec.Command("git", "fetch", "--tags")
	gitFetch.Dir = gitRepoDir
	gitFetch.Stdout = os.Stderr
	gitFetch.Stderr = os.Stderr
	if err := gitFetch.Run(); err != nil {
		return semver.Version{}, false, err
	}

	gitDescribe := exec.Command("git", "tag")
	if driver.Branch != "" {
		gitDescribe.Args = append(gitDescribe.Args, fmt.Sprintf(`--merged=origin/%s`, driver.Branch))
	}
	gitDescribe.Args = append(gitDescribe.Args, "-l", driver.Prefix+"*")
	gitDescribe.Dir = gitRepoDir
	describeOutput, err := gitDescribe.CombinedOutput()
	describeOutputStr := string(describeOutput)
	if err != nil {
		if strings.Contains(describeOutputStr, nothingToDescribe) ||
			strings.Contains(describeOutputStr, notAValidObject) {
			os.Stderr.Write(describeOutput)
			return semver.Version{}, false, nil
		}

		os.Stderr.Write(describeOutput)
		return semver.Version{}, false, err
	}

	currentVersion, err := currentVersion(driver.Prefix, describeOutputStr)

	if err != nil {
		os.Stderr.Write(describeOutput)
		return semver.Version{}, false, err
	}

	return currentVersion, true, nil
}

func (driver *GitTagDriver) writeVersion(newVersion semver.Version, params map[string]interface{}) (bool, error) {
	tagMessage := fmt.Sprintf(
		"\"Pipeline: %s\\nJob: %s\\nBuild: %s\"",
		os.Getenv("BUILD_PIPELINE_NAME"),
		os.Getenv("BUILD_JOB_NAME"),
		os.Getenv("BUILD_NAME"))

	gitFetch := exec.Command("git", "fetch", "--tags", "--dry-run", "--depth=1")
	gitFetch.Dir = gitRepoDir
	gitFetchOutput, err := gitFetch.CombinedOutput()
	if err != nil {
		os.Stderr.Write(gitFetchOutput)
		return false, err
	}

	var headRef string
	if params != nil {
		repo, ok := params["repo"]
		if !ok {
			os.Stderr.Write([]byte("The repo parameter is required for the git-tag driver"))
		}

		repoStr := repo.(string)
		os.Stderr.Write([]byte("Use the sha from resource: " + repoStr))

		gitLs := exec.Command("git",
			"--git-dir="+path.Join(repoStr, ".git"),
			"log", "-1", `--pretty=format:"%H"`)
		gitLsOutput, err := gitLs.CombinedOutput()
		if err != nil {
			os.Stderr.Write(gitLsOutput)
			return false, err
		}

		headRef, err = strconv.Unquote(strings.TrimSpace(string(gitLsOutput)))
		if err != nil {
			os.Stderr.Write(gitLsOutput)
			return false, err
		}
	} else {
		at := "HEAD"
		if driver.Branch != "" {
			at = driver.Branch
		}

		os.Stderr.Write([]byte("Use the last hash at " + at))

		gitLs := exec.Command("git", "ls-remote", "origin", at)
		gitLs.Dir = gitRepoDir
		gitLsOutput, err := gitLs.CombinedOutput()
		if err != nil {
			os.Stderr.Write(gitLsOutput)
			return false, err
		}

		headRef = strings.Split(string(gitLsOutput), "\t")[0]
	}

	version := driver.Prefix + newVersion.String()

	gitTag := exec.Command("git", "tag", "--force", "--annotate", "--message", tagMessage, version, headRef)
	gitTag.Dir = gitRepoDir
	tagOutput, err := gitTag.CombinedOutput()
	if err != nil {
		os.Stderr.Write(tagOutput)
		return false, err
	}

	gitShowTag := exec.Command("git", "ls-remote", "origin", fmt.Sprintf("refs/tags/%s", version))
	gitShowTag.Dir = gitRepoDir
	showTagOutput, err := gitShowTag.CombinedOutput()

	if err != nil {
		os.Stderr.Write(showTagOutput)
		return false, err
	}

	if strings.Contains(string(showTagOutput), version) {
		gitDeleteTag := exec.Command("git", "push", "origin", fmt.Sprintf(":refs/tags/%s", version))
		gitDeleteTag.Dir = gitRepoDir
		deleteTagOutput, err := gitDeleteTag.CombinedOutput()

		if err != nil {
			os.Stderr.Write(deleteTagOutput)
			return false, err
		}
	}

	gitPushTag := exec.Command("git", "push", "origin", version)
	gitPushTag.Dir = gitRepoDir

	pushTagOutput, err := gitPushTag.CombinedOutput()

	if strings.Contains(string(pushTagOutput), pushRejectedString) {
		return false, nil
	}

	if strings.Contains(string(pushTagOutput), pushRemoteRejectedString) {
		return false, nil
	}

	if err != nil {
		os.Stderr.Write(pushTagOutput)
		return false, err
	}

	return true, nil
}

func (driver *GitTagDriver) setUpRepo() error {

	if driver.Repository != "" {
		_, err := os.Stat(driver.Repository)
		if err == nil {
			return err
		}
		gitRepoDir = driver.Repository
	} else if driver.URI != "" {
		_, err := os.Stat(gitRepoDir)
		if err != nil {
			// Init an empty repo ...
			gitInit := exec.Command("git", "init", gitRepoDir)
			gitInit.Stdout = os.Stderr
			gitInit.Stderr = os.Stderr
			if err := gitInit.Run(); err != nil {
				return err
			}
			// ... and setup the remote
			gitRemote := exec.Command("git", "remote", "add", "origin", driver.URI)
			gitRemote.Dir = gitRepoDir
			gitRemote.Stdout = os.Stderr
			gitRemote.Stderr = os.Stderr
			if err := gitRemote.Run(); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("Expected either repository (path) or URI to be configured.")
	}

	if driver.Prefix == "" {
		driver.Prefix = "v"
	}

	return nil
}
