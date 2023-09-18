/*
Copyright 2023 Stefan Prodan

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"cuelang.org/go/cue/cuecontext"
	"github.com/fluxcd/pkg/ssa"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/stefanprodan/timoni/internal/engine"
)

var importCrdCmd = &cobra.Command{
	Use:   "crd [MODULE PATH]",
	Short: "Generate CUE definitions from Kubernetes CRDs",
	Example: `  # generate CUE definitions from a local YAML file
  timoni mod import crd -f crds.yaml
`,
	RunE: runImportCrdCmd,
}

type importCrdFlags struct {
	modRoot string
	crdFile string
}

var importCrdArgs importCrdFlags

func init() {
	importCrdCmd.Flags().StringVarP(&importCrdArgs.crdFile, "file", "f", "",
		"The path to Kubernetes CRD YAML.")

	modImportCmd.AddCommand(importCrdCmd)
}

const header = `// Code generated by timoni. DO NOT EDIT.

//timoni:generate timoni import crd -f `

func runImportCrdCmd(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		importCrdArgs.modRoot = args[0]
	}

	log := LoggerFrom(cmd.Context())
	cuectx := cuecontext.New()

	// Make sure we're importing into a CUE module.
	cueModDir := path.Join(importCrdArgs.modRoot, "cue.mod")
	if fs, err := os.Stat(cueModDir); err != nil || !fs.IsDir() {
		return fmt.Errorf("cue.mod not found in the module path %s", importCrdArgs.modRoot)
	}

	// Load the YAML file into memory.
	var crdData []byte
	if fs, err := os.Stat(importCrdArgs.crdFile); err != nil || !fs.Mode().IsRegular() {
		return fmt.Errorf("path not found: %s", importCrdArgs.crdFile)
	}

	f, err := os.Open(importCrdArgs.crdFile)
	if err != nil {
		return err
	}

	crdData, err = io.ReadAll(f)
	if err != nil {
		return err
	}

	// Extract the Kubernetes CRDs from the multi-doc YAML.
	var builder strings.Builder
	objects, err := ssa.ReadObjects(bytes.NewReader(crdData))
	if err != nil {
		return fmt.Errorf("parsing CRDs failed: %w", err)
	}
	for _, object := range objects {
		if object.GetKind() == "CustomResourceDefinition" {
			builder.WriteString("---\n")
			data, err := yaml.Marshal(object)
			if err != nil {
				return fmt.Errorf("marshaling CRD failed: %w", err)
			}
			builder.Write(data)
		}
	}

	// Generate the CUE definitions from the given CRD YAML.
	imp := engine.NewImporter(cuectx, fmt.Sprintf("%s%s", header, importCrdArgs.crdFile))
	crds, err := imp.Generate([]byte(builder.String()))
	if err != nil {
		return err
	}

	// Sort the resulting definitions based on file names.
	keys := make([]string, 0, len(crds))
	for k := range crds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Write the definitions to the module's 'cue.mod/gen' dir.
	for _, k := range keys {
		log.Info(fmt.Sprintf("generating: %s", colorizeSubject(k)))

		dstDir := path.Join(cueModDir, "gen", k)
		if err := os.MkdirAll(dstDir, os.ModePerm); err != nil {
			return err
		}

		if err := os.WriteFile(path.Join(dstDir, "types_gen.cue"), crds[k], 0644); err != nil {
			return err
		}
	}

	return nil
}
