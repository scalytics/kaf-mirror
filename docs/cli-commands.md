# mirror-cli

A CLI for managing kaf-mirror.

## Description

mirror-cli is a command-line tool for managing kaf-mirror.
It interacts with the kaf-mirror API to perform various tasks.

## Global Options

```
  -, --mirror-url string   URL of the kaf-mirror backend. (default "http://localhost:8080")
  -v, --version   Print the version and exit
```

## Available Commands

### mirror-cli clusters

**Manage clusters.**

The clusters command allows you to list, add, remove, and purge clusters.

### Usage

```
mirror-cli clusters
```

### Available Subcommands

- **add** - Add a new cluster.
- **edit** - Edit an existing cluster. Empty credential prompts keep the stored secret (API responses never return real secrets).
- **list** - List all clusters.
- **purge** - Purge archived clusters.
- **remove** - Mark a cluster for deletion.
- **restore** - Restore an inactive cluster.
- **test** - Test connection to a cluster.

#### mirror-cli clusters add

**Add a new cluster.**

This command adds a new Kafka cluster with the specified name and brokers.

### Usage

```
mirror-cli clusters add
```

#### mirror-cli clusters edit

**Edit an existing cluster.**

This command allows you to edit the name and brokers of an existing Kafka cluster.

### Usage

```
mirror-cli clusters edit [name]
```

#### mirror-cli clusters list

**List all clusters.**

This command lists all configured Kafka clusters.

### Usage

```
mirror-cli clusters list
```

#### mirror-cli clusters purge

**Purge archived clusters.**

This command permanently deletes all archived Kafka clusters.

### Usage

```
mirror-cli clusters purge
```

#### mirror-cli clusters remove

**Mark a cluster for deletion.**

This command marks a cluster as inactive. It will be archived after 72 hours and then can be purged.

### Usage

```
mirror-cli clusters remove [name]
```

#### mirror-cli clusters restore

**Restore an inactive cluster.**

This command restores an inactive cluster to active status.

### Usage

```
mirror-cli clusters restore [name]
```

#### mirror-cli clusters test

**Test connection to a cluster.**

This command tests the connection to a specified Kafka cluster and provides detailed diagnostics.

### Usage

```
mirror-cli clusters test [name]
```

### mirror-cli config

**Manage application configuration**

Configure and manage kaf-mirror settings

### Usage

```
mirror-cli config
```

### Available Subcommands

- **edit** - Edit configuration parameters
- **get** - Display current configuration
- **save** - Save configuration from YAML file to server
- **system** - Initial system configuration wizard

#### mirror-cli config edit

**Edit configuration parameters**

Hierarchical configuration editing with validation

### Usage

```
mirror-cli config edit
```

### Available Subcommands

- **ai** - Configure AI settings
- **compliance** - Configure compliance reporting schedule
- **monitoring** - Configure monitoring settings
- **replication** - Configure replication settings
- **server** - Configure server settings

##### mirror-cli config edit ai

**Configure AI settings**

Configure AI provider, model, and features

### Usage

```
mirror-cli config edit ai
```

### Available Subcommands

- **features** - Enable/disable AI features
- **manage** - Manage AI configuration interactively
- **model** - Set AI model
- **provider** - Set AI provider and configure endpoints
- **self-healing** - Configure AI self-healing features
- **test-and-enable** - Test AI connection and automatically enable all features

###### mirror-cli config edit ai features

**Enable/disable AI features**

Configure which AI features are enabled: anomaly_detection, performance_optimization, incident_analysis

### Usage

```
mirror-cli config edit ai features
```

###### mirror-cli config edit ai manage

**Manage AI configuration interactively**

View current AI configuration and modify settings interactively

### Usage

```
mirror-cli config edit ai manage
```

###### mirror-cli config edit ai model

**Set AI model**

### Usage

```
mirror-cli config edit ai model [model]
```

###### mirror-cli config edit ai provider

**Set AI provider and configure endpoints**

### Usage

```
mirror-cli config edit ai provider [provider]
```

###### mirror-cli config edit ai self-healing

**Configure AI self-healing features**

Enable or disable automated remediation and dynamic throttling.

### Usage

```
mirror-cli config edit ai self-healing
```

###### mirror-cli config edit ai test-and-enable

**Test AI connection and automatically enable all features**

Test the configured AI provider and automatically enable all AI features if connection is successful

### Usage

```
mirror-cli config edit ai test-and-enable
```

##### mirror-cli config edit compliance

**Configure compliance reporting schedule**

Configure automated compliance report scheduling

### Usage

```
mirror-cli config edit compliance
```

### Available Subcommands

- **daily** - Enable or disable daily compliance reports
- **enabled** - Enable or disable compliance scheduling
- **manage** - Manage compliance scheduling interactively
- **monthly** - Enable or disable monthly compliance reports (1st of month)
- **run-hour** - Set report run hour (0-23 local time)
- **weekly** - Enable or disable weekly compliance reports (Monday)

###### mirror-cli config edit compliance daily

**Enable or disable daily compliance reports**

### Usage

```
mirror-cli config edit compliance daily [value]
```

###### mirror-cli config edit compliance enabled

**Enable or disable compliance scheduling**

### Usage

```
mirror-cli config edit compliance enabled [value]
```

###### mirror-cli config edit compliance manage

**Manage compliance scheduling interactively**

### Usage

```
mirror-cli config edit compliance manage
```

###### mirror-cli config edit compliance monthly

**Enable or disable monthly compliance reports (1st of month)**

### Usage

```
mirror-cli config edit compliance monthly [value]
```

###### mirror-cli config edit compliance run-hour

**Set report run hour (0-23 local time)**

### Usage

```
mirror-cli config edit compliance run-hour [hour]
```

###### mirror-cli config edit compliance weekly

**Enable or disable weekly compliance reports (Monday)**

### Usage

```
mirror-cli config edit compliance weekly [value]
```

##### mirror-cli config edit monitoring

**Configure monitoring settings**

Configure monitoring platform and endpoints

### Usage

```
mirror-cli config edit monitoring
```

### Available Subcommands

- **enabled** - Enable or disable monitoring
- **manage** - Manage monitoring configuration interactively
- **platform** - Set monitoring platform

###### mirror-cli config edit monitoring enabled

**Enable or disable monitoring**

### Usage

```
mirror-cli config edit monitoring enabled [value]
```

###### mirror-cli config edit monitoring manage

**Manage monitoring configuration interactively**

View current monitoring configuration and modify settings interactively

### Usage

```
mirror-cli config edit monitoring manage
```

###### mirror-cli config edit monitoring platform

**Set monitoring platform**

### Usage

```
mirror-cli config edit monitoring platform [platform]
```

##### mirror-cli config edit replication

**Configure replication settings**

Configure batch size, parallelism, compression, and topic discovery interval

### Usage

```
mirror-cli config edit replication
```

### Available Subcommands

- **batch-size** - Set replication batch size (KB)
- **compression** - Set compression algorithm
- **discovery-interval** - Set topic discovery interval
- **parallelism** - Set replication parallelism

###### mirror-cli config edit replication batch-size

**Set replication batch size (KB)**

### Usage

```
mirror-cli config edit replication batch-size [size]
```

###### mirror-cli config edit replication compression

**Set compression algorithm**

### Usage

```
mirror-cli config edit replication compression [type]
```

###### mirror-cli config edit replication discovery-interval

**Set topic discovery interval**

### Usage

```
mirror-cli config edit replication discovery-interval [duration]
```

###### mirror-cli config edit replication parallelism

**Set replication parallelism**

### Usage

```
mirror-cli config edit replication parallelism [count]
```

##### mirror-cli config edit server

**Configure server settings**

Configure server host, port, mode, TLS, and CORS settings

### Usage

```
mirror-cli config edit server
```

### Available Subcommands

- **cors** - Configure CORS allowed origins
- **host** - Set server host address
- **manage** - Manage server configuration interactively
- **mode** - Set server mode
- **port** - Set server port
- **tls** - Configure server TLS/HTTPS settings

###### mirror-cli config edit server cors

**Configure CORS allowed origins**

### Usage

```
mirror-cli config edit server cors [origins]
```

###### mirror-cli config edit server host

**Set server host address**

### Usage

```
mirror-cli config edit server host [address]
```

###### mirror-cli config edit server manage

**Manage server configuration interactively**

View current server configuration and modify settings interactively

### Usage

```
mirror-cli config edit server manage
```

###### mirror-cli config edit server mode

**Set server mode**

### Usage

```
mirror-cli config edit server mode [mode]
```

###### mirror-cli config edit server port

**Set server port**

### Usage

```
mirror-cli config edit server port [port]
```

###### mirror-cli config edit server tls

**Configure server TLS/HTTPS settings**

### Usage

```
mirror-cli config edit server tls [enabled]
```

#### mirror-cli config get

**Display current configuration**

Show the current configuration from the server

### Usage

```
mirror-cli config get
```

#### mirror-cli config save

**Save configuration from YAML file to server**

Load and apply configuration from a YAML file, replacing server configuration

### Usage

```
mirror-cli config save [file]
```

#### mirror-cli config system

**Initial system configuration wizard**

Run the initial configuration wizard to set up kaf-mirror

### Usage

```
mirror-cli config system
```

### mirror-cli dashboard

**Show a live monitoring dashboard.**

### Usage

```
mirror-cli dashboard
```

### mirror-cli docs

**Generate CLI documentation**

Generate CLI documentation in various formats using Cobra's built-in documentation generation

### Usage

```
mirror-cli docs
```

### Available Subcommands

- **generate** - Generate single comprehensive Markdown documentation file

#### mirror-cli docs generate

**Generate single comprehensive Markdown documentation file**

Generate a single comprehensive Markdown file with all CLI commands and subcommands

### Usage

```
mirror-cli docs generate [flags]
```

### Options

```
  -, --file string   Output file for documentation (default "docs/cli-commands.md")
```

### mirror-cli jobs

**Manage replication jobs**

Create, start, stop, and manage Kafka replication jobs

### Usage

```
mirror-cli jobs
```

### Available Subcommands

- **add** - Create a new replication job
- **analyze** - Trigger historical AI analysis for a job
- **delete** - Delete a replication job
- **force-restart** - Forcefully restart a replication job
- **healthcheck** - Perform health checks on clusters associated with a job
- **list** - List all replication jobs
- **pause** - Pause a replication job
- **restart** - Restart a replication job
- **start** - Start a replication job
- **status** - Show detailed job status and metrics
- **stop** - Stop a replication job

#### mirror-cli jobs add

**Create a new replication job**

Create a new replication job with interactive configuration

### Usage

```
mirror-cli jobs add
```

#### mirror-cli jobs analyze

**Trigger historical AI analysis for a job**

Trigger a long-term historical AI analysis for a replication job

### Usage

```
mirror-cli jobs analyze [job-id] [flags]
```

### Options

```
  -, --period string   Analysis period (e.g., 7d, 30d) (default "7d")
```

#### mirror-cli jobs delete

**Delete a replication job**

Permanently delete a replication job (admin only)

### Usage

```
mirror-cli jobs delete [job-id]
```

#### mirror-cli jobs force-restart

**Forcefully restart a replication job**

Forcefully restarts a job, performing pre-flight checks to ensure a clean start.

### Usage

```
mirror-cli jobs force-restart [job-id]
```

#### mirror-cli jobs healthcheck

**Perform health checks on clusters associated with a job**

Performs health checks on the source and target clusters for a specific job or all jobs.

### Usage

```
mirror-cli jobs healthcheck [job-id]
```

#### mirror-cli jobs list

**List all replication jobs**

Display all configured replication jobs with their status

### Usage

```
mirror-cli jobs list
```

#### mirror-cli jobs pause

**Pause a replication job**

Pause a running replication job (can be resumed later)

### Usage

```
mirror-cli jobs pause [job-id]
```

#### mirror-cli jobs restart

**Restart a replication job**

Restart a replication job (stop and start it)

### Usage

```
mirror-cli jobs restart [job-id]
```

#### mirror-cli jobs start

**Start a replication job**

Start a paused or stopped replication job

### Usage

```
mirror-cli jobs start [job-id]
```

#### mirror-cli jobs status

**Show detailed job status and metrics**

Display detailed status and performance metrics for a replication job

### Usage

```
mirror-cli jobs status
```

### Available Subcommands

- **full** - Show full mirror state for a job
- **metrics** - Show detailed job status and metrics

##### mirror-cli jobs status full

**Show full mirror state for a job**

Display the full mirror state for a replication job, including progress, gaps, and resume points.

### Usage

```
mirror-cli jobs status full [job-id]
```

##### mirror-cli jobs status metrics

**Show detailed job status and metrics**

Display detailed status and performance metrics for a replication job

### Usage

```
mirror-cli jobs status metrics [job-id]
```

#### mirror-cli jobs stop

**Stop a replication job**

Stop a running replication job

### Usage

```
mirror-cli jobs stop [job-id]
```

### mirror-cli login

**Login to the kaf-mirror backend**

### Usage

```
mirror-cli login [username] [flags]
```

### Options

```
  -, --password string   User's password
```

### mirror-cli logout

**Logout from the kaf-mirror backend.**

### Usage

```
mirror-cli logout
```

### mirror-cli tls

**Manage TLS certificates and secure communication**

Generate certificates and configure secure TLS communication between CLI and server

### Usage

```
mirror-cli tls
```

### Available Subcommands

- **configure-client** - Configure CLI client for TLS communication
- **generate-certs** - Generate TLS certificates for server and client
- **import-cert** - Import custom TLS certificate and key
- **verify** - Verify TLS connection to server

#### mirror-cli tls configure-client

**Configure CLI client for TLS communication**

Configure the CLI to use client certificates for secure communication

### Usage

```
mirror-cli tls configure-client
```

#### mirror-cli tls generate-certs

**Generate TLS certificates for server and client**

Generate self-signed TLS certificates for secure CLI-to-server communication

### Usage

```
mirror-cli tls generate-certs
```

#### mirror-cli tls import-cert

**Import custom TLS certificate and key**

Import your own TLS certificate and private key for the server

### Usage

```
mirror-cli tls import-cert
```

#### mirror-cli tls verify

**Verify TLS connection to server**

Test the TLS connection and certificate validation

### Usage

```
mirror-cli tls verify
```

### mirror-cli users

**Manage users.**

The users command allows you to list, add, and manage users.

### Usage

```
mirror-cli users
```

### Available Subcommands

- **add** - Add a new user.
- **change-password** - Change the current user's password (minimum 12 characters). Existing tokens are revoked.
- **delete** - Delete a user.
- **list** - List all users.
- **reset-password** - Reset a user's password (admin only).
- **reset-token** - Reset your API token.
- **set-role** - Set a user's role.

#### mirror-cli users add

**Add a new user.**

This command adds a new user with the specified username and password.

### Usage

```
mirror-cli users add [flags]
```

### Options

```
  -, --password string   User's password
```

#### mirror-cli users change-password

**Change the current user's password.**

### Usage

```
mirror-cli users change-password
```

#### mirror-cli users delete

**Delete a user.**

This command deletes a user from the database.

### Usage

```
mirror-cli users delete [username]
```

#### mirror-cli users list

**List all users.**

This command lists all users.

### Usage

```
mirror-cli users list
```

#### mirror-cli users reset-password

**Reset a user's password (admin only).**

This command allows admins to reset any user's password and generates a new random password.

### Usage

```
mirror-cli users reset-password [username] [flags]
```

### Options

```
  -, --password-file string   Write new password to file instead of stdout
```

#### mirror-cli users reset-token

**Reset your API token.**

This command revokes all of your existing API tokens and generates a new one.

### Usage

```
mirror-cli users reset-token
```

#### mirror-cli users set-role

**Set a user's role.**

This command sets the role for the specified user.

### Usage

```
mirror-cli users set-role [username] [role]
```

### mirror-cli whoami

**Display the current user's information.**

### Usage

```
mirror-cli whoami
```

---

*Auto generated by spf13/cobra on 19-Jan-2026*
