// The DotNet SDK and runtime.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"path"
	"text/template"

	"dagger/dotnet-sdk/internal/dagger"

	"github.com/iancoleman/strcase"
)

//go:embed templates/Program.cs
var mainProg string

//go:embed templates/MainModule.cs
var mainModule string

const (
	ModSourceDirPath = "/src"
)

var (
	IgnorePaths = []string{
		"**/introspection.json",
		"**/bin",
		"**/obj",
	}
)

func New(
	// +optional
	sdkSourceDir *dagger.Directory,
) *DotnetSdk {
	if sdkSourceDir == nil {
		sdkSourceDir = dag.CurrentModule().Source().Directory("sdk")
	}
	return &DotnetSdk{
		SDKSourceDir:  sdkSourceDir,
		RequiredPaths: []string{},
		Container:     dag.Container(),
	}
}

type DotnetSdk struct {
	SDKSourceDir  *dagger.Directory
	RequiredPaths []string
	Container     *dagger.Container
}

func (m *DotnetSdk) ModuleRuntime(
	ctx context.Context,
	modSource *dagger.ModuleSource,
	introspectionJson *dagger.File,
) (*dagger.Container, error) {
	subpath, err := modSource.SourceSubpath(ctx)
	if err != nil {
		return nil, err
	}

	modName, err := modSource.ModuleName(ctx)
	if err != nil {
		return nil, err
	}
	name := strcase.ToCamel(modName)

	m, err = m.codegenBase(ctx, modSource, introspectionJson)
	if err != nil {
		return nil, err
	}

	return m.Container.WithEntrypoint([]string{"dotnet", "run", "--project", path.Join(ModSourceDirPath, subpath, name)}), nil
}

func (m *DotnetSdk) Codegen(
	ctx context.Context,
	modSource *dagger.ModuleSource,
	introspectionJson *dagger.File,
) (*dagger.GeneratedCode, error) {
	m, err := m.codegenBase(ctx, modSource, introspectionJson)
	if err != nil {
		return nil, err
	}

	return dag.GeneratedCode(m.Container.Directory(ModSourceDirPath)).
		WithVCSGeneratedPaths([]string{"Dagger.SDK*/**"}).
		WithVCSIgnoredPaths([]string{"Dagger.SDK*/**", "**/obj", "**/bin", "**/.idea"}), nil
}

func (m *DotnetSdk) codegenBase(ctx context.Context, modSource *dagger.ModuleSource, introspectionJson *dagger.File) (*DotnetSdk, error) {
	modName, err := modSource.ModuleName(ctx)
	if err != nil {
		return nil, err
	}
	subpath, err := modSource.SourceSubpath(ctx)
	if err != nil {
		return nil, err
	}

	return m.
		WithBase(modSource.ContextDirectory(), subpath).
		WithSln(modName).
		WithSdk(subpath).
		WithIntrospection(introspectionJson).
		WithProject(ctx, subpath, modName)
}

func (m *DotnetSdk) WithBase(contextDir *dagger.Directory, subpath string) *DotnetSdk {
	m.Container = m.Container.
		From("mcr.microsoft.com/dotnet/sdk:8.0-alpine3.20").
		WithMountedDirectory(ModSourceDirPath, contextDir).
		WithWorkdir(path.Join(ModSourceDirPath, subpath))
	return m
}

func (m *DotnetSdk) WithSln(modName string) *DotnetSdk {
	name := strcase.ToCamel(modName)
	m.Container = m.Container.WithExec([]string{"dotnet", "new", "sln", "--name", name, "--force"})
	return m
}

// Installing sdk into subpath.
func (m *DotnetSdk) WithSdk(subpath string) *DotnetSdk {
	m.Container = m.Container.
		WithDirectory(
			"Dagger.SDK",
			m.SDKSourceDir.Directory("Dagger.SDK"),
			dagger.ContainerWithDirectoryOpts{Exclude: IgnorePaths},
		).
		WithDirectory(
			"Dagger.SDK.Mod.SourceGenerator",
			m.SDKSourceDir.Directory("Dagger.SDK.Mod.SourceGenerator"),
			dagger.ContainerWithDirectoryOpts{Exclude: IgnorePaths},
		).
		WithDirectory(
			"Dagger.SDK.SourceGenerator/Dagger.SDK.SourceGenerator",
			m.SDKSourceDir.Directory("Dagger.SDK.SourceGenerator/Dagger.SDK.SourceGenerator"),
			dagger.ContainerWithDirectoryOpts{Exclude: IgnorePaths},
		).
		WithExec([]string{"dotnet", "sln", "add", "Dagger.SDK"}).
		WithExec([]string{"dotnet", "sln", "add", "Dagger.SDK.Mod.SourceGenerator"}).
		WithExec([]string{"dotnet", "sln", "add", "Dagger.SDK.SourceGenerator/Dagger.SDK.SourceGenerator"})

	return m
}

func (m *DotnetSdk) WithIntrospection(introspectionJson *dagger.File) *DotnetSdk {
	m.Container = m.Container.
		WithFile("Dagger.SDK/introspection.json", introspectionJson)

	return m
}

func (m *DotnetSdk) WithProject(ctx context.Context, subpath string, modName string) (*DotnetSdk, error) {
	ctr := m.Container
	name := strcase.ToCamel(modName)

	ents, err := m.Container.Directory(".").Entries(ctx)
	if err != nil {
		return nil, err
	}
	created := false
	for _, ent := range ents {
		if ent == name {
			created = true
			break
		}
	}

	if !created {
		ctr = ctr.
			WithExec([]string{"dotnet", "new", "console", "--framework", "net8.0", "--output", name, "-n", name}).
			WithExec([]string{"dotnet", "add", name, "reference", "Dagger.SDK"}).
			WithExec([]string{"dotnet", "add", name, "reference", "Dagger.SDK.Mod.SourceGenerator"})
	}

	var buf bytes.Buffer
	err = template.Must(template.New("Program.cs").Parse(mainProg)).Execute(&buf, struct{ Module string }{Module: name})
	if err != nil {
		return nil, err
	}

	prog := buf.String()
	buf.Reset()

	err = template.Must(template.New("MainModule.cs").Parse(mainModule)).Execute(&buf, struct{ Module string }{Module: name})
	if err != nil {
		return nil, err
	}

	mainMod := buf.String()

	m.Container = ctr.
		WithExec([]string{"dotnet", "sln", "add", name}).
		WithNewFile(name+"/Program.cs", prog).
		WithNewFile(name+"/"+name+".cs", mainMod)
	return m,
		nil
}
