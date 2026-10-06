package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// gitResolve returns the commit a branch (or tag) points to, using Git's
// smart HTTP protocol (what `git ls-remote` does), so the backend doesn't
// need git installed or a clone.
func gitResolve(ctx context.Context, repoURL, ref string) (string, error) {
	if err := validateGitURL(repoURL); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	url := strings.TrimSuffix(repoURL, "/") + "/info/refs?service=git-upload-pack"
	resp, err := safeGet(ctx, url, http.Header{"Git-Protocol": {"version=0"}})
	if err != nil {
		return "", fmt.Errorf("could not reach the repository: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return "", errors.New("repository not found. Check the URL; private repositories aren't supported yet")
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "git-upload-pack") {
		return "", fmt.Errorf("%s doesn't look like a Git repository (HTTP %d)", repoURL, resp.StatusCode)
	}

	want := map[string]bool{"refs/heads/" + ref: true, "refs/tags/" + ref + "^{}": true, "refs/tags/" + ref: true}
	found := ""
	r := bufio.NewReader(io.LimitReader(resp.Body, 16<<20))
	for {
		line, err := readPktLine(r)
		if err != nil {
			break
		}
		// "<sha> <ref>\x00capabilities" or "<sha> <ref>"
		line, _, _ = strings.Cut(strings.TrimSuffix(line, "\n"), "\x00")
		sha, name, ok := strings.Cut(line, " ")
		if !ok || len(sha) != 40 {
			continue
		}
		if _, err := hex.DecodeString(sha); err != nil {
			continue
		}
		if want[name] {
			found = sha
			if strings.HasPrefix(name, "refs/heads/") || strings.HasSuffix(name, "^{}") {
				return sha, nil
			}
		}
	}
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("branch %q not found in the repository", ref)
}

// readPktLine reads one pkt-line: 4 hex digits of length, then data.
// "0000" (flush) lines return "".
func readPktLine(r *bufio.Reader) (string, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", err
	}
	var n int
	if _, err := fmt.Sscanf(string(hdr[:]), "%04x", &n); err != nil {
		return "", err
	}
	if n <= 4 {
		return "", nil
	}
	buf := make([]byte, n-4)
	_, err := io.ReadFull(r, buf)
	return string(buf), err
}

// pollGitDeployments redeploys deployments with auto deploy on when their
// branch moves to a new commit.
func (s *Server) pollGitDeployments() {
	for range time.Tick(s.cfg.GitPollEvery) {
		rows, err := s.db.Query(`SELECT d.id, p.user_id, COALESCE(r.git_commit, '') FROM deployments d
			JOIN projects p ON p.id = d.project_id
			LEFT JOIN deployment_revisions r ON r.id = d.current_revision_id
			WHERE d.auto_deploy AND d.spec->'source'->>'kind' = 'git' AND d.status NOT IN ('DELETING', 'STOPPED')
			  AND NOT EXISTS (SELECT 1 FROM agent_tasks t WHERE t.deployment_id = d.id AND t.type = 'DEPLOY' AND t.status IN ('PENDING', 'DISPATCHED'))`)
		if err != nil {
			log.Printf("git poll: %v", err)
			continue
		}
		type candidate struct{ id, uid, commit string }
		var list []candidate
		for rows.Next() {
			var c candidate
			if rows.Scan(&c.id, &c.uid, &c.commit) == nil {
				list = append(list, c)
			}
		}
		rows.Close()

		for _, c := range list {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			d, err := s.getDeploymentByID(ctx, c.id)
			if err == nil {
				var latest string
				latest, err = gitResolve(ctx, d.Spec.Source.GitURL, d.Spec.Source.GitRef)
				if err == nil && latest != c.commit {
					if _, err = s.deploy(ctx, d, d.Spec, "auto", "", latest, nil); err == nil {
						s.event(ctx, c.uid, "info", fmt.Sprintf("New commit %s on %s: redeploying %s", latest[:7], d.Spec.Source.GitRef, d.Name), withDeployment(d))
					}
				}
			}
			if err != nil {
				log.Printf("git poll %s: %v", c.id, err)
			}
			cancel()
		}
	}
}
