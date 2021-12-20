// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package publish

import (
	"github.com/google/ko/internal/publish"
	"github.com/google/ko/pkg/publish/daemon"
	"github.com/google/ko/pkg/publish/remote"
	"github.com/google/ko/pkg/publish/tarball"
)

// Interface abstracts different methods for publishing images.
type Interface publish.Interface

// Namer is a function from a supported import path to the portion of the resulting
// image name that follows the "base" repository name.
type Namer publish.Namer

/// Remote types and methods.

var NewDefault = remote.New

type Option remote.Option

var WithTransport = remote.WithTransport
var WithUserAgent = remote.WithUserAgent
var WithAuth = remote.WithAuth
var WithAuthFromKeychain = remote.WithAuthFromKeychain
var WithNamer = remote.WithNamer
var WithTags = remote.WithTags
var WithTagOnly = remote.WithTagOnly
var Insecure = remote.Insecure

/// Daemon types and methods.

type DaemonOption daemon.Option

const LocalDomain = daemon.LocalDomain

var WithLocalDomain = daemon.WithLocalDomain
var WithDockerClient = daemon.WithDockerClient
var NewDaemon = daemon.New

/// Tarball types and methods.

var NewTarball = tarball.New
