package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	v1 "github.com/ob-labs/powercontext-go/api/v1"
	pcclient "github.com/ob-labs/powercontext-go/client"
	"github.com/spf13/cobra"
)

func newExperienceCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "experience", Short: "Generate managed Experience Candidates."}
	command.AddCommand(newGenerateExperienceCommand(state))
	return command
}

func newGenerateExperienceCommand(state *commandState) *cobra.Command {
	var scopeID, target, reason string
	var sourceRefs, artifactRefs []string
	command := &cobra.Command{
		Use: "generate", Short: "Generate at most one pending Experience Candidate.", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			sources, artifacts, targetRef, err := evidenceReferences(sourceRefs, artifactRefs, target)
			if err != nil {
				return usageError(err)
			}
			if err := requireTargetFamily(targetRef, "experience"); err != nil {
				return usageError(err)
			}
			request := &v1.GenerateExperienceRequest{
				ScopeID: scopeID, SourceRefs: sources, ArtifactRefs: artifacts,
				Target: targetRef, Reason: optionalString(reason),
			}
			return state.execute(command, func(ctx context.Context, client *pcclient.Client) (any, error) {
				return client.GenerateExperience(ctx, request)
			})
		},
	}
	bindEvidenceFlags(command, &scopeID, &sourceRefs, &artifactRefs, &target, &reason)
	return command
}

func newSkillCommand(state *commandState) *cobra.Command {
	command := &cobra.Command{Use: "skill", Short: "Generate, inspect, and export managed Skills."}
	command.AddCommand(newGenerateSkillCommand(state), newShowSkillCommand(state), newExportSkillCommand(state))
	return command
}

func newGenerateSkillCommand(state *commandState) *cobra.Command {
	var scopeID, origin, target, reason string
	var sourceRefs, artifactRefs []string
	command := &cobra.Command{
		Use: "generate", Short: "Generate at most one pending managed Skill Candidate.", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			sources, artifacts, targetRef, err := evidenceReferences(sourceRefs, artifactRefs, target)
			if err != nil {
				return usageError(err)
			}
			if err := validateSkillOrigin(v1.SkillGenerationOrigin(origin), sources, artifacts, targetRef); err != nil {
				return usageError(err)
			}
			request := &v1.GenerateSkillRequest{
				ScopeID: scopeID, Origin: v1.SkillGenerationOrigin(origin),
				SourceRefs: sources, ArtifactRefs: artifacts, Target: targetRef, Reason: optionalString(reason),
			}
			return state.execute(command, func(ctx context.Context, client *pcclient.Client) (any, error) {
				return client.GenerateSkill(ctx, request)
			})
		},
	}
	bindEvidenceFlags(command, &scopeID, &sourceRefs, &artifactRefs, &target, &reason)
	command.Flags().StringVar(&origin, "origin", "", "Provenance shape: experience, source, or usage.")
	_ = command.MarkFlagRequired("origin")
	return command
}

func newShowSkillCommand(state *commandState) *cobra.Command {
	var scopeID string
	var revision int
	command := &cobra.Command{
		Use: "show ARTIFACT_ID", Short: "Read one exact approved managed Skill Revision.", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if revision < 1 {
				return usageError(errors.New("--revision must be at least 1"))
			}
			request := &v1.GetSkillRequest{ScopeID: scopeID, Artifact: v1.ArtifactReference{
				Family: "skill", ArtifactID: args[0], Revision: revision,
			}}
			return state.execute(command, func(ctx context.Context, client *pcclient.Client) (any, error) {
				return client.GetSkill(ctx, request)
			})
		},
	}
	command.Flags().StringVar(&scopeID, "scope-id", "", "Application scope containing the managed Skill.")
	command.Flags().IntVar(&revision, "revision", 0, "Exact managed Skill Revision.")
	_ = command.MarkFlagRequired("scope-id")
	_ = command.MarkFlagRequired("revision")
	return command
}

func newExportSkillCommand(state *commandState) *cobra.Command {
	var scopeID, target, destination string
	var revision int
	command := &cobra.Command{
		Use: "export ARTIFACT_ID", Short: "Export one exact approved Revision for an Agent integration target.", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if target != "codex" {
				return usageError(errors.New("--target must be codex"))
			}
			if revision < 1 {
				return usageError(errors.New("--revision must be at least 1"))
			}
			request := &v1.GetSkillRequest{ScopeID: scopeID, Artifact: v1.ArtifactReference{
				Family: "skill", ArtifactID: args[0], Revision: revision,
			}}
			body, err := state.call(command.Context(), func(ctx context.Context, client *pcclient.Client) (any, error) {
				return client.GetSkill(ctx, request)
			})
			if err != nil {
				return err
			}
			value, ok := body.(v1.SkillArtifact)
			if !ok {
				return fmt.Errorf("unexpected Skill response %T", body)
			}
			exported, err := projectCodexSkill(value, destination)
			if err != nil {
				return usageError(fmt.Errorf("cannot export managed Skill for codex: %w", err))
			}
			_, err = fmt.Fprintf(state.stdout, "Exported %s@%d for codex to %s\n", value.Artifact.ArtifactID, value.Artifact.Revision, exported)
			return err
		},
	}
	command.Flags().StringVar(&scopeID, "scope-id", "", "Application scope containing the managed Skill.")
	command.Flags().StringVar(&target, "target", "", "Agent integration target: codex.")
	command.Flags().StringVar(&destination, "destination", "", "New target Skill directory; existing paths are never replaced.")
	command.Flags().IntVar(&revision, "revision", 0, "Exact managed Skill Revision.")
	for _, name := range []string{"scope-id", "target", "destination", "revision"} {
		_ = command.MarkFlagRequired(name)
	}
	return command
}

func bindEvidenceFlags(
	command *cobra.Command,
	scopeID *string,
	sourceRefs, artifactRefs *[]string,
	target, reason *string,
) {
	command.Flags().StringVar(scopeID, "scope-id", "", "Application scope receiving the generated Candidate.")
	command.Flags().StringArrayVar(sourceRefs, "source-ref", nil, "Exact Source as TYPE/ID; repeat for more evidence.")
	command.Flags().StringArrayVar(artifactRefs, "artifact-ref", nil, "Exact Artifact as FAMILY/ID@REVISION; repeat for more evidence.")
	command.Flags().StringVar(target, "target", "", "Exact replacement/evolution target as FAMILY/ID@REVISION.")
	command.Flags().StringVar(reason, "reason", "", "Why this generation is requested.")
	_ = command.MarkFlagRequired("scope-id")
}

func validateSkillOrigin(
	origin v1.SkillGenerationOrigin,
	sources []v1.SourceReference,
	artifacts []v1.ArtifactReference,
	target v1.OptNilArtifactReference,
) error {
	targetValue, hasTarget := target.Get()
	switch origin {
	case v1.SkillGenerationOriginExperience:
		if hasTarget || len(artifacts) == 0 {
			return errors.New("experience origin requires Experience refs and no target")
		}
		for _, ref := range artifacts {
			if ref.Family != "experience" {
				return errors.New("experience origin requires Experience refs and no target")
			}
		}
	case v1.SkillGenerationOriginSource:
		if hasTarget || len(sources) == 0 || len(artifacts) != 0 {
			return errors.New("source origin requires only Source refs")
		}
	case v1.SkillGenerationOriginUsage:
		if !hasTarget || targetValue.Family != "skill" || len(sources) == 0 {
			return errors.New("usage origin requires a target Skill and Source refs")
		}
	default:
		return errors.New("--origin must be experience, source, or usage")
	}
	return nil
}

const codexProjectionSchema = "powercontext.codex-skill-projection.v1"

var codexSkillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func projectCodexSkill(value v1.SkillArtifact, destination string) (string, error) {
	if value.Artifact.Family != "skill" {
		return "", errors.New("artifact must identify a managed Skill")
	}
	target, err := filepath.Abs(destination)
	if err != nil {
		return "", errors.New("destination is invalid")
	}
	content := value.Content
	if len(content.Name) > 64 || !codexSkillName.MatchString(content.Name) {
		return "", errors.New("managed Skill name must be at most 64 lowercase letters, digits, and single hyphens for Codex")
	}
	if filepath.Base(target) != content.Name {
		return "", errors.New("Codex skill directory name must match the managed Skill name")
	}
	if len([]rune(content.Description)) > 1_024 || strings.ContainsAny(content.Description, "<>") {
		return "", errors.New("managed Skill description must be at most 1024 characters and contain no angle brackets for Codex")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", errors.New("cannot create destination parent")
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", errors.New("destination already exists")
		}
		return "", errors.New("cannot create destination")
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(target)
		}
	}()
	markdown, err := codexSkillMarkdown(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(markdown))
	manifest := struct {
		Artifact    v1.ArtifactReference `json:"artifact"`
		Schema      string               `json:"schema"`
		SkillSHA256 string               `json:"skill_sha256"`
	}{value.Artifact, codexProjectionSchema, hex.EncodeToString(digest[:])}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte(markdown), 0o644); err != nil {
		return "", errors.New("cannot write Skill projection")
	}
	if err := os.WriteFile(filepath.Join(target, "powercontext.json"), manifestBytes, 0o644); err != nil {
		return "", errors.New("cannot write Skill projection manifest")
	}
	committed = true
	return target, nil
}

func codexSkillMarkdown(value v1.SkillArtifact) (string, error) {
	name, err := jsonString(value.Content.Name)
	if err != nil {
		return "", err
	}
	description, err := jsonString(value.Content.Description)
	if err != nil {
		return "", err
	}
	var validation strings.Builder
	for _, item := range value.Content.Validation {
		validation.WriteString("- ")
		validation.WriteString(string(item))
		validation.WriteByte('\n')
	}
	return fmt.Sprintf(
		"---\nname: %s\ndescription: %s\n---\n\n<!-- Generated from artifact:%s/%s@%d. The Artifact Revision remains authoritative. -->\n\n%s\n\n## Validation\n\n%s",
		name, description, value.Artifact.Family, value.Artifact.ArtifactID, value.Artifact.Revision,
		strings.TrimRight(value.Content.Instructions, " \t\r\n"), validation.String(),
	), nil
}

func jsonString(value string) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
