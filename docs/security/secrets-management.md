# Secrets management

## Why care?

Real teams report problems like these:

- "We deleted the key from the repo last year. Why did someone just use it?"
- "The database password is the same in dev, test and prod, and nobody knows who has it."
- "We have to rotate the payments key tonight. Which of our 40 services use it?"

A secret is easy to create and very hard to take back. Once it is copied (into Git, a log, an image, a laptop) you can't know every place it went. So the goal of secrets management is not "hide the password well". It is:

1. **Fewer secrets.** Use identity instead of passwords when you can.
2. **Short-lived secrets.** A credential that expires in an hour is worth little to a thief.
3. **One place.** Every secret lives in one store, with an audit log and access rules.
4. **Easy rotation.** Changing a secret is a routine, tested, one-command job.

On this page you run HashiCorp Vault in a container, get database passwords that delete themselves, encrypt a config file with `sops`, and write a small Go scanner that finds a key in Git history after it was "deleted".

## The idea: short-lived credentials from one store

A **static secret**, such as a password in a config file, works until someone changes it. It gets copied to people, scripts and machines, and after some years nobody knows how many copies exist. To be safe, you must change it and then update everyone who still needs it.

A **secrets manager** such as Vault works differently:

- It creates a credential when a client asks, for that client only.
- The credential expires after a set time.
- If a credential leaks, the secrets manager revokes it and issues a new one. Other clients are not affected.
- It keeps an audit log of every credential it issued.

The secrets manager knows **who** is asking (identity), decides **what** they may access (policy), and gives out credentials that **expire** (leases).

```mermaid
sequenceDiagram
    participant O as ordersd
    participant V as Vault
    participant DB as Postgres
    O->>V: login (Kubernetes token, cloud identity...)
    V-->>O: Vault token, TTL 1h, policy orders-read
    O->>V: GET database/creds/orders-app
    V->>DB: CREATE ROLE v-token-... VALID UNTIL +1m
    V-->>O: username, password, lease 1m
    O->>DB: connect with that user
    Note over V,DB: lease ends: Vault drops the role
    O->>V: renew lease, or ask for new creds
```

## Under the hood: what is a secret, and where does it leak?

### What counts as a secret

A **secret** is any value that gives access when you have it. No other check. Examples: passwords, API keys, OAuth client secrets, private keys, session-signing keys (HMAC keys, see [Cryptography](cryptography.md)), database connection strings with a password inside, webhook signing secrets, cloud access keys.

Some things are **not** secrets, even if they feel private: a public key, a username, an internal hostname, a database name. Keep them out of logs if you like, but don't put them in the secret store. A store full of non-secrets makes the real ones harder to find and rotate.

### The five usual leaks

| Where      | How it happens                                                              | What fixes it                                                 |
| ---------- | --------------------------------------------------------------------------- | ------------------------------------------------------------- |
| **Git**    | Committed "just for a test", then deleted. History keeps it forever         | Never commit; scan before push; rotate when it happens        |
| **Logs**   | `slog.Info("config", "cfg", cfg)`, a panic dump, a debug `Authorization`    | A `Secret` type that won't print; log IDs, not values         |
| **Env**    | Visible in `/proc/<pid>/environ`, `docker inspect`, crash reports, children | Files with tight modes, read when needed                      |
| **Images** | `COPY .env` then `rm`, or a build `ARG` with a token                        | Build secrets (`RUN --mount=type=secret`), never in a layer   |
| **CI**     | Secrets printed by a step, or stolen by a malicious dependency or action    | Short-lived OIDC credentials, pinned actions, least privilege |

The CI row is the bridge to the next page, [Supply chain security](supply-chain.md). The log row is covered in depth in [What never goes into a log](../observability/structured-logging.md#what-never-goes-into-a-log), and the env-vs-files question in [Files or environment variables?](../kubernetes/config-and-secrets.md#files-or-environment-variables). Here are the two that surprise people most.

### Leak 1: the environment is not private

Start Vault in dev mode (more on that below) with its root token in an environment variable, then ask Docker about the container:

```console
$ docker inspect tutor-vault --format '{{json .Config.Env}}'
["VAULT_DEV_ROOT_TOKEN_ID=tutor-root-token","VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200","PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin","NAME=vault"]
```

Anyone who can run `docker inspect` (or `kubectl get pod -o yaml`, or read the Deployment in Git) now has the root token. Vault even prints the environment variable **names** in its own start-up banner. Environment variables are fine for **config**. For secrets, prefer a file, or better, a credential the process fetches itself.

### Leak 2: `rm` in a Dockerfile deletes nothing

This is a common mistake. The build needs a key, so the Dockerfile copies it in, uses it, and removes it:

```dockerfile
FROM hashicorp/vault:1.21
ARG DEPLOY_TOKEN
COPY .env /app/.env
RUN echo "using $DEPLOY_TOKEN" > /dev/null && rm /app/.env
```

```console
$ docker build -q --build-arg DEPLOY_TOKEN=tutor_fake_key_fedcba9876543210 -t tutor-leaky:dev .
DEPRECATED: The legacy builder is deprecated and will be removed in a future release.
            Install the buildx component to build images with BuildKit:
            https://docs.docker.com/go/buildx/

sha256:9ee4da422e32cde9f63e96b3753a66b63a88198f33e7624f8bbce50881fe81cf
$ docker run --rm --entrypoint ls tutor-leaky:dev -la /app
total 8
drwxr-xr-x    1 root     root          4096 Nov 27 11:24 .
drwxr-xr-x    1 root     root          4096 Nov 27 11:24 ..
```

The file is gone from the final filesystem. But an image is a stack of layers (see [Building good images](../containers/building-images.md)), and the `COPY` layer still holds it. The `ARG` value is in the image history, in plain text:

```console
$ docker history --no-trunc --format '{{.CreatedBy}}' tutor-leaky:dev | head -3
|1 DEPLOY_TOKEN=tutor_fake_key_fedcba9876543210 /bin/sh -c echo "using $DEPLOY_TOKEN" > /dev/null && rm /app/.env
/bin/sh -c #(nop) COPY file:483737b5bd3014c270ff95ba6fd1d4dcda855cfccd60c897c11d20e5bb142e50 in /app/.env
/bin/sh -c #(nop)  ARG DEPLOY_TOKEN
$ mkdir img && docker save tutor-leaky:dev | tar -x -C img
$ for l in $(jq -r '.[0].Layers[]' img/manifest.json); do
>   tar -tf img/$l | grep -q '^app/.env$' && { echo "found in $l"; tar -xOf img/$l app/.env; }
> done
found in blobs/sha256/f4ec1ea0e73919776cba50ed5fe1f452dc6f2103a872359bdbbc54ecbae2dff8
API_KEY=tutor_fake_key_0123456789abcdef
```

Anyone who can pull the image can read both. The fix is BuildKit's **build secrets**: `RUN --mount=type=secret,id=deploy_token ...` mounts the secret as a file for that one `RUN` step, and it is never written to a layer or to the history. (This machine only has Docker's legacy builder, which prints a deprecation warning and has no `--mount`; with `docker buildx` it works the same everywhere.) Even better: make the build not need a secret at all, for example by downloading private modules through a company proxy that trusts the CI identity.

## Under the hood: Vault

[HashiCorp Vault](https://developer.hashicorp.com/vault) (and its open-source fork OpenBao) is the most common self-hosted secrets manager. Its ideas are the same in the cloud stores, so it is a good model to learn.

### Start a dev server

**Dev mode** keeps everything in memory, starts unsealed, and has a root token you choose. It is useful for learning. Never use it for anything real.

```sh
docker run -d --rm --name tutor-vault --cap-add=IPC_LOCK -p 127.0.0.1:8200:8200 \
  -e VAULT_DEV_ROOT_TOKEN_ID=tutor-root-token -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 \
  hashicorp/vault:1.21

# Run the vault CLI that lives inside the container. VT picks the token; default is root.
vault() { docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN="${VT:-tutor-root-token}" tutor-vault vault "$@"; }
```

```console
$ vault status | grep -E 'Sealed|Version|Storage'
Sealed          false
Version         1.21.4
Storage Type    inmem
$ vault secrets list
Path          Type         Accessor              Description
----          ----         --------              -----------
cubbyhole/    cubbyhole    cubbyhole_ab5d6257    per-token private secret storage
identity/     identity     identity_f88a1d11     identity store
secret/       kv           kv_3292cfad           key/value secret storage
sys/          system       system_b8ad720a       system endpoints used for control, policy and debugging
```

Everything in Vault is a **path**, and every path belongs to a **secrets engine** mounted at a prefix. `secret/` is a key-value store. `sys/` is Vault's own control plane. More engines are mounted below.

A real server starts **sealed**: its storage is encrypted, and the key to decrypt it is split into shares (Shamir's secret sharing) or held by a cloud KMS ("auto-unseal"). Until enough shares are given, Vault can't read its own data. That's the answer to "who protects the secrets manager?".

### KV: a versioned key-value store

```console
$ vault kv put secret/orders/db username=orders password=tutor-fake-pw-v1
==== Secret Path ====
secret/data/orders/db

======= Metadata =======
Key                Value
---                -----
created_time       2026-11-27T11:22:00.934612909Z
custom_metadata    <nil>
deletion_time      n/a
destroyed          false
version            1
$ vault kv put secret/orders/db username=orders password=tutor-fake-pw-v2
...
version            2
$ vault kv get -version=1 -field=password secret/orders/db
tutor-fake-pw-v1
```

Two details:

- The CLI path is `secret/orders/db`, but the **API** path is `secret/data/orders/db`. KV version 2 adds `data/` (the value) and `metadata/` (versions, who changed it). Policies use the API path. Forgetting `data/` in a policy is the most common Vault mistake.
- Old versions stay. That's good for rollback after a bad rotation, and it means "I overwrote the password" does not remove the old one. `vault kv destroy` does.

### Policies: deny by default

A new token can do nothing until a policy allows it. Here is the whole policy for `ordersd`:

```hcl
# ordersd may read its own secrets, and nothing else.
path "secret/data/orders/*" {
  capabilities = ["read"]
}
```

```console
$ docker cp orders-read.hcl tutor-vault:/tmp/orders-read.hcl
$ vault policy write orders-read /tmp/orders-read.hcl
Success! Uploaded policy: orders-read
$ vault token create -policy=orders-read -ttl=2m -field=token > tok.txt
$ VT=$(cat tok.txt) vault token lookup | grep -E 'policies|ttl|renewable'
creation_ttl        2m
explicit_max_ttl    0s
policies            [default orders-read]
renewable           true
ttl                 1m59s
$ VT=$(cat tok.txt) vault kv get -field=password secret/orders/db
tutor-fake-pw-v2
$ VT=$(cat tok.txt) vault kv put secret/orders/db password=hacked
Error writing data to secret/data/orders/db: Error making API request.

URL: PUT http://127.0.0.1:8200/v1/secret/data/orders/db
Code: 403. Errors:

* 1 error occurred:
	* permission denied
$ VT=$(cat tok.txt) vault kv get secret/payments/stripe
Error reading secret/data/payments/stripe: Error making API request.

URL: GET http://127.0.0.1:8200/v1/secret/data/payments/stripe
Code: 403. Errors:

* 1 error occurred:
	* permission denied
```

Read its own path: yes. Write: no. Someone else's path: no. This is the same "deny by default, allow the minimum" rule as in [Authorization](authorization.md), applied to secrets. The token itself expires in two minutes.

### Dynamic secrets: passwords that delete themselves

KV still stores a **static** password that someone created and someone must rotate. The **database secrets engine** goes further: Vault holds an admin login for Postgres and creates a **new database user for every request**, with an expiry.

Start a Postgres next to Vault (a throwaway one, on its own Docker network):

```sh
docker network create tutor-secrets
docker network connect tutor-secrets tutor-vault
docker run -d --rm --name tutor-vault-pg --network tutor-secrets \
  -e POSTGRES_PASSWORD=tutor-fake-admin-pw -e POSTGRES_DB=orders postgres:18
```

Tell Vault how to connect, and what a new user looks like:

```console
$ vault secrets enable database
Success! Enabled the database secrets engine at: database/
$ vault write database/config/orders plugin_name=postgresql-database-plugin \
>   'connection_url=postgresql://{{username}}:{{password}}@tutor-vault-pg:5432/orders?sslmode=disable' \
>   username=postgres password=tutor-fake-admin-pw allowed_roles=orders-app
Success! Data written to: database/config/orders
$ vault write database/roles/orders-app db_name=orders default_ttl=1m max_ttl=5m \
>   "creation_statements=CREATE ROLE \"{{name}}\" WITH LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expiration}}'; GRANT SELECT ON ALL TABLES IN SCHEMA public TO \"{{name}}\";"
Success! Data written to: database/roles/orders-app
```

Now ask for credentials:

```console
$ vault read database/creds/orders-app
Key                Value
---                -----
lease_id           database/creds/orders-app/CLOmjvF0BE8V7b3MivQwFD6P
lease_duration     1m
lease_renewable    true
password           poXWSyb7RydHDHP-I2Pt
username           v-token-orders-a-JSJKnyZWun4TrIEAfJmm-1795778557
$ docker exec tutor-vault-pg psql -U postgres -d orders -c "select rolname, rolvaliduntil from pg_roles where rolname like 'v-%'"
                     rolname                      |     rolvaliduntil
--------------------------------------------------+------------------------
 v-token-orders-a-JSJKnyZWun4TrIEAfJmm-1795778557 | 2026-11-27 11:23:42+00
(1 row)
```

A real Postgres role, made for this one request, valid for one minute. The **lease** is Vault's record of it. Wait a minute:

```console
$ date -u +%T
11:23:50
$ docker exec tutor-vault-pg psql -U postgres -d orders -c "select rolname from pg_roles where rolname like 'v-%'"
 rolname
---------
(0 rows)

$ docker logs tutor-vault 2>&1 | grep 'revoked lease.*CLOmjvF0'
2026-11-27T11:23:37.208Z [INFO]  expiration: revoked lease: lease_id=database/creds/orders-app/CLOmjvF0BE8V7b3MivQwFD6P
```

The user is gone. Nobody rotated anything. If this password leaks into a log tomorrow, it opens nothing.

A running service doesn't want new credentials every minute, so it **renews** the lease, up to `max_ttl`:

```console
$ vault lease renew -increment=4m database/creds/orders-app/rHR4Ib32FoqwNSRiAJ89e9Ir
Key                Value
---                -----
lease_id           database/creds/orders-app/rHR4Ib32FoqwNSRiAJ89e9Ir
lease_duration     4m
lease_renewable    true
$ vault lease renew -increment=10m database/creds/orders-app/rHR4Ib32FoqwNSRiAJ89e9Ir
WARNING! The following warnings were returned from Vault:

  * TTL of "10m" exceeded the effective max_ttl of "5m"; TTL value is capped
  accordingly
...
```

After `max_ttl`, the service must ask for a **new** user. That's a feature: every credential has a hard end of life.

Vault can also revoke credentials in an incident. Suppose the `orders` credentials leaked. Revoke all of them at once:

```console
$ docker exec tutor-vault-pg psql -U postgres -d orders -tAc "select count(*) from pg_roles where rolname like 'v-%'"
2
$ vault lease revoke -prefix database/creds/orders-app
All revocation operations queued successfully!
$ docker exec tutor-vault-pg psql -U postgres -d orders -tAc "select count(*) from pg_roles where rolname like 'v-%'"
0
```

Compare that with "find every config file that has the password, change it everywhere, restart everything", during an incident.

### Rotate the root, too

One problem remains: we typed the Postgres admin password `tutor-fake-admin-pw` into Vault. It's in our shell history. Let Vault change it to a value only Vault knows:

```console
$ vault write -f database/rotate-root/orders
Success! Data written to: database/rotate-root/orders
$ docker run --rm --name tutor-psql --network tutor-secrets -e PGPASSWORD=tutor-fake-admin-pw \
>   postgres:18 psql -h tutor-vault-pg -U postgres -d orders -c 'select 1'
psql: error: connection to server at "tutor-vault-pg" (172.19.0.3), port 5432 failed: FATAL:  password authentication failed for user "postgres"
```

The old admin password is dead. No human knows the new one. In production, use a dedicated `vault` admin role in Postgres instead of `postgres` itself, so a break-glass superuser login still exists, kept offline.

### From Go: fetch credentials at start-up

Vault's API is plain HTTP and JSON, so a stdlib client is short. The policy needs one more path, `database/creds/orders-app` with `read`. The token comes from a **file** (written by an agent or a login step), not from an environment variable:

```go
req, _ := http.NewRequest(http.MethodGet, addr+"/v1/database/creds/orders-app", nil)
req.Header.Set("X-Vault-Token", strings.TrimSpace(string(token)))
client := &http.Client{Timeout: 5 * time.Second}
resp, err := client.Do(req)
// ... check resp.StatusCode, decode lease_id, lease_duration, data.username, data.password

ttl := time.Duration(c.LeaseDuration) * time.Second
fmt.Printf("user=%s password=<%d bytes, not printed>\n", c.Data.Username, len(c.Data.Password))
fmt.Printf("lease=%s ttl=%s renewable=%t\n", c.LeaseID, ttl, c.Renewable)
// Renew (or fetch new credentials) at about two thirds of the TTL,
// so a slow Vault or a retry still finishes before the old ones expire.
fmt.Printf("renew at %s\n", time.Now().Add(ttl*2/3).UTC().Format(time.TimeOnly))
```

```console
$ date -u +%T
15:42:14
$ VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN_FILE=tok3.txt ./vaultget
user=v-token-orders-a-c2vDkW0CeA9L6CdzwSJc-1795794134 password=<20 bytes, not printed>
lease=database/creds/orders-app/9fbDmreRu16glmbhZlEWRIy7 ttl=1m0s renewable=true
renew at 15:42:54
$ echo bad > bad.txt
$ VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN_FILE=bad.txt ./vaultget
vault: 403 Forbidden
```

In a real service, the database pool needs to handle the change: open new connections with the new user, and close old connections before the old user expires (`sql.DB.SetConnMaxLifetime` shorter than the lease helps). Many teams let **Vault Agent** (or the Vault Secrets Operator in Kubernetes) do the login and renewal, and write the credentials to a file the service re-reads. Then the Go code only reads a file, as in [Config and secrets](../kubernetes/config-and-secrets.md#files-or-environment-variables).

### Keep secrets out of your own logs

Once the password is in a Go variable, the next leak is your own `slog` call. A small type that refuses to print helps a lot:

```go
// Secret is a string that refuses to be printed.
type Secret string

func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) LogValue() slog.Value         { return slog.StringValue("[REDACTED]") }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (s Secret) Reveal() string             { return string(s) } // the only way out: easy to grep for

type DBConfig struct {
	User     string
	Password Secret
}
```

Log a plain struct and the `DBConfig`, then print it every way `fmt` knows:

```console
$ go run .
{"time":"2026-11-27T16:42:23.570671939+01:00","level":"INFO","msg":"config loaded","db":{"User":"orders","Password":"tutor-fake-pw-v2"}}
{"time":"2026-11-27T16:42:23.57083572+01:00","level":"INFO","msg":"config loaded","db":{"User":"orders","Password":"[REDACTED]"}}
{orders [REDACTED]}
{User:orders Password:[REDACTED]}
main.DBConfig{User:"orders", Password:[REDACTED]}
{"User":"orders","Password":"[REDACTED]"}
16
```

The first line is the usual mistake. Every other line is safe, because `fmt`, `slog` and `encoding/json` all ask the type how to show itself. `Reveal()` is the one place the real value comes out, and a reviewer can grep for it. (It does not stop `string(cfg.Password)` or reflection. It reduces accidental leaks; it is not a security boundary.)

### Workload identity

Vault gave `ordersd` a token. But how does `ordersd` get **that** token without a secret? This is the "secret zero" problem: every secret store needs a first credential.

The answer is **workload identity**: the platform already knows who the workload is, and signs a short-lived token that says so.

- In **Kubernetes**, every pod gets a projected service account token, a JWT signed by the API server with the pod's namespace and service account (see [Every pod has an identity](../kubernetes/config-and-secrets.md#every-pod-has-an-identity)). Vault's **Kubernetes auth method** checks that JWT with the API server and returns a Vault token with the policy for that service account. No password anywhere.
- In the **cloud**, the VM or pod gets an identity from the instance metadata service (AWS IAM roles, GCP service accounts, Azure managed identities). Vault's AWS, GCP and Azure auth methods accept those, and so do the cloud's own secret stores.
- In **CI**, GitHub Actions and GitLab issue an OIDC token per job ("repo `acme/orders`, branch `main`, workflow `deploy.yml`"). Cloud providers and Vault's JWT auth method trade it for short-lived credentials. No long-lived cloud key in the CI settings.

The pattern is always the same: **identity in, short-lived credential out**. The JWT checks are the ones from [Authentication](authentication.md): issuer, audience, expiry, signature.

### Cloud secret stores

AWS Secrets Manager, Google Secret Manager and Azure Key Vault do the same job as Vault's KV, as managed services:

- Access is controlled by the cloud's IAM, and every read is in the cloud audit log.
- Values are encrypted with a KMS key. Separate "may read the secret" from "may use the key" for extra safety.
- **Rotation** is built in for some databases (AWS runs a Lambda function on a schedule, for example). Dynamic per-request users like Vault's are less common.
- In Kubernetes, the External Secrets Operator or the Secrets Store CSI Driver brings them into pods (see [Secrets that don't start in Kubernetes](../kubernetes/config-and-secrets.md#secrets-that-dont-start-in-kubernetes)).

If you run in one cloud, its store plus workload identity is usually the simplest choice. Vault earns its place with many clouds, on-premises systems, or dynamic secrets.

## Under the hood: encrypted files with `sops`

Sometimes a secret must live in Git: a Helm values file, a Terraform variable, a config for a system that can't call Vault. **SOPS** encrypts the **values** in a YAML, JSON, `.env` or INI file and leaves the keys readable, so diffs and reviews still work.

How it works: SOPS makes a random **data key**, encrypts each value with AES-256-GCM, and then encrypts the data key with one or more **master keys**: a cloud KMS key, a PGP or age key, or a Vault **transit** key. Transit is "encryption as a service": Vault holds the key and never gives it out. Use it here, because it ties into the policies above.

!!! tip "New tool: `sops`"

    ```sh
    mise use sops@3.13.3
    ```

    This adds it to the project's `mise.toml`, pinned to an exact version. See [Adding tools as you go](../toolbox/mise.md#adding-tools-as-you-go).

```console
$ vault secrets enable transit
Success! Enabled the transit secrets engine at: transit/
$ vault write -f transit/keys/orders-sops >/dev/null
$ vault read transit/keys/orders-sops | grep -E '^(type|exportable)'
exportable                false
type                      aes256-gcm96
$ cat secrets.yaml
db:
  user: orders
  password: tutor-fake-pw-v2
payments_key: tutor_fake_key_0123456789abcdef
log_level: info
$ export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=tutor-root-token
$ sops encrypt --hc-vault-transit http://127.0.0.1:8200/v1/transit/keys/orders-sops \
>   --encrypted-regex '^(password|payments_key)$' secrets.yaml > secrets.enc.yaml
$ cat secrets.enc.yaml
db:
    user: orders
    password: ENC[AES256_GCM,data:Nmuq9ZHLBPh5w5pncVC90A==,iv:7I+ml0F/QLS2QqejPFsTjVfkww2ScuHP9YNsZQKRu9A=,tag:O0SwcLLzQFRji+PQ3/KKpg==,type:str]
payments_key: ENC[AES256_GCM,data:6nQ4BMmWTJFXtEZyCCIXodMy+NsfJs8z6BS41b7jPg==,iv:OMke4POloa53trvi9Q4MHuTic4TstsklaSw1TCm+G5Q=,tag:ogSNJSnP7xL60xbxzhfm1Q==,type:str]
log_level: info
sops:
    encrypted_regex: ^(password|payments_key)$
    hc_vault:
        - created_at: "2026-11-27T11:25:28Z"
          enc: vault:v1:cBbBuMdW1p7oO6jehlO07IArZrShHNoNKQAK6P5SmYuha60E5NIvOp3Q2W2tN3mUwhI4WKwvoii3lGH7
          engine_path: transit
          key_name: orders-sops
          vault_address: http://127.0.0.1:8200
    lastmodified: "2026-11-27T11:25:28Z"
    mac: ENC[AES256_GCM,data:mxs+6YklVoqnRGY7rD4HTwWQZfeJMvz2llJ8ft2qIz/hIntiSRQa4h4LsSShlb7KCPsnMIsrCSVzFIBwpUnY8c2/toDij3o2MGelLgsCgnfWcBIjNsftgY8p38Zp8GweWHh6I9wrEy9d9jMPkPceN8mr3sRLUoOfT6nPG/giapk=,iv:2KA9dUf/Tf/3HQZGY2oA07ious5KBG/PMAv7+fzuck4=,tag:dT+qb5i2pxibUKc6MXZUKw==,type:str]
    version: 3.13.3
```

Read it from the bottom up:

- `hc_vault.enc` is the data key, encrypted by Vault (`vault:v1:` means version 1 of the transit key).
- Each `ENC[...]` is one value: ciphertext, a fresh 12-byte IV (nonce), and a GCM tag. AEAD, as on the [Cryptography](cryptography.md) page.
- `mac` covers the **whole** file, including the plain `log_level`. Change any value and SOPS refuses to decrypt:

```console
$ sops decrypt --extract '["db"]["password"]' secrets.enc.yaml
tutor-fake-pw-v2
$ sed 's/log_level: info/log_level: debug/' secrets.enc.yaml > tampered.yaml
$ sops decrypt tampered.yaml; echo exit=$?
MAC mismatch. File has 7327381896D9EAC432E3011ADD9B10AB19ECDBBCF8BB4DF6C8BEDF68C248727B336EA48CC0DE93310E97543979EE58528E1F49E9F9D1649D540D80BEDC848543, computed CEBAEA077757B479BA0C920A9F7C7E925A4F6531995EAC6BB84C9E8C68DFAFA872AE938987D32E1F3D1034E1EC068617E84E8304D99492C8926268F02FBCD913
exit=51
```

And who may decrypt is a **Vault policy**, not "who has the file". The `ordersd` token has no `transit/decrypt` right:

```console
$ VAULT_TOKEN=$(cat tok2.txt) sops decrypt secrets.enc.yaml; echo exit=$?
Failed to get the data key required to decrypt the SOPS file.

Group 0: FAILED
  http://127.0.0.1:8200/v1/transit/keys/orders-sops: FAILED
    - | failed to decrypt sops data key from Vault transit backend
      | 'transit/decrypt/orders-sops': Error making API request.
      |
      | URL: PUT
      | http://127.0.0.1:8200/v1/transit/decrypt/orders-sops
      | Code: 403. Errors:
      |
      | * 1 error occurred:
      | 	* permission denied
      |


Recovery failed because no master key was able to decrypt the file. In
order for SOPS to recover the file, at least one key has to be successful,
but none were.
exit=128
```

Without Vault or a cloud KMS, use a PGP or age key. With PGP, in a throwaway keyring:

```console
$ export GNUPGHOME=$(mktemp -d); chmod 700 $GNUPGHOME
$ gpg --batch --quiet --passphrase '' --quick-gen-key 'tutor-sops <sops@example.com>' ed25519 cert,sign 1d
gpg: checking the trustdb
gpg: marginals needed: 3  completes needed: 1  trust model: pgp
gpg: depth: 0  valid:   1  signed:   0  trust: 0-, 0q, 0n, 0m, 0f, 1u
gpg: next trustdb check due at 2026-11-28
$ FP=$(gpg --list-keys --with-colons tutor-sops | awk -F: '/^fpr/{print $10; exit}')
$ gpg --batch --quiet --passphrase '' --quick-add-key $FP cv25519 encr 1d
$ echo $FP
F79C57AD1E3DAE6A9E33047612B78430CBFFEAC2
$ sops encrypt --pgp $FP secrets.yaml > secrets.pgp.yaml
$ sops decrypt --extract '["payments_key"]' secrets.pgp.yaml
tutor_fake_key_0123456789abcdef
```

Now the private PGP key is the secret you must protect, and every person who should decrypt needs their own key listed in the file (`sops updatekeys` after changing `.sops.yaml`). That's why teams prefer KMS or Vault transit: access is a policy you can change, and removing a person does not mean re-encrypting every file. Either way, **rotating the master key does not rotate the secrets**: someone who decrypted the file last month still knows `payments_key`.

## Under the hood: rotation

Rotation means: make a new secret, move every user to it, then kill the old one. The order matters. Do it in the wrong order and you have an outage.

```mermaid
flowchart LR
    A[1. Create new<br/>v2 valid] --> B[2. Both valid<br/>v1 and v2]
    B --> C[3. Move clients<br/>to v2]
    C --> D[4. Check nobody<br/>uses v1]
    D --> E[5. Revoke v1]
```

- **Two valid at once.** Systems that accept two keys (two API keys per client, a JWKS with two signing keys, Postgres with two users) make rotation safe. A system with exactly one password per account can't rotate without a gap. Design for two.
- **Clients must reload.** A service that reads the secret once at start-up needs a restart. One that re-reads a file or renews a lease does not.
- **Measure step 4.** Use the access log of the provider, or Vault's audit log, to see who still uses the old key. Don't guess.
- **Rotate on a schedule**, so the process is tested before you need it in an incident. Dynamic secrets do this for you: every lease is a rotation.

When a secret **leaks**, the order is: **rotate first**, then clean up. Removing a key from Git is cosmetic. Revoking it is the fix.

## Try it: find a "deleted" key in Git history

Make a throwaway repository. Commit a config, add a key, then "remove" it:

```console
$ git init -q -b main && echo 'addr: ":8081"' > config.yaml && git add . && git commit -qm "add config"
$ printf 'addr: ":8081"\npayments_key: tutor_fake_key_0123456789abcdef\n' > config.yaml && git commit -qam "wire up payments"
$ echo 'addr: ":8081"' > config.yaml && git commit -qam "remove key"
$ git log --oneline
cae1cb0 remove key
f19016b wire up payments
40ada39 add config
$ grep -r tutor_fake . --exclude-dir=.git; echo "grep exit=$?"
grep exit=1
```

The working tree is clean. History is not:

```console
$ git log --all --oneline -S tutor_fake_key
cae1cb0 remove key
f19016b wire up payments
$ git show f19016b:config.yaml
addr: ":8081"
payments_key: tutor_fake_key_0123456789abcdef
```

`-S` (the "pickaxe") lists commits that change the number of times a string appears: the one that added it and the one that removed it. Every clone, fork and CI cache has this history.

A scanner does the same for many patterns at once. Here is a tiny one in Go. It reads `git log --all -p` and checks every **added** line:

```go
// secretscan finds tutor-format fake keys anywhere in a Git repository's
// history: every line ever added, on every branch, with its commit and file.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// The tutor's fake key format. Real scanners ship hundreds of these rules,
// one per provider, plus an entropy check for "random-looking" strings.
var rule = regexp.MustCompile(`tutor_fake_key_[0-9a-f]{16}`)

func main() {
	// -p: show patches. --all: every ref, not just HEAD. --format: one
	// marker line per commit, so we know where each patch came from.
	cmd := exec.Command("git", "log", "--all", "-p", "--no-color", "--format=commit %h %s")
	out, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var commit, file string
	found := 0
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "commit "):
			commit = strings.TrimPrefix(line, "commit ")
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if key := rule.FindString(line); key != "" {
				found++
				fmt.Printf("%s  %s  %s...\n", commit, file, key[:len(key)-12]) // never print the whole secret
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if found > 0 {
		fmt.Printf("%d secret(s) found\n", found)
		os.Exit(1)
	}
	fmt.Println("no secrets found")
}
```

```console
$ ~/tutor/scan/secretscan; echo exit=$?
f19016b wire up payments  config.yaml  tutor_fake_key_0123...
1 secret(s) found
exit=1
```

Found, with the commit and the file, and only a prefix of the key in the output (a scanner's report is itself a log). The non-zero exit code lets CI fail the build.

Real tools work the same way, with hundreds of provider rules (AWS, GitHub, Stripe, Slack...), entropy checks, and allow-lists for test data:

!!! tip "New tool: `gitleaks`"

    ```sh
    mise use gitleaks@8.30.1
    ```

    This adds it to the project's `mise.toml`, pinned to an exact version. See [Adding tools as you go](../toolbox/mise.md#adding-tools-as-you-go).

```sh
gitleaks git .                       # scan history
gitleaks dir .                       # scan the working tree
trufflehog git file://. --only-verified   # also asks the provider if the key is live
```

<!-- TODO(real-output): gitleaks and trufflehog are not installed on this machine; run them on the leak repo and show their findings. -->

Where to run a scanner:

1. **Pre-commit hook** on the developer machine (see [hooks](../git/config-hooks-signing.md#under-the-hood-hooks)). Fast, but optional: anyone can skip it with `--no-verify`.
2. **CI on every pull request**, on the new commits. This one is enforced.
3. **Push protection** on the Git server (GitHub secret scanning, GitLab secret detection): the push is refused before the key is public. The best place, because the secret never lands.
4. **A full history scan** once, when you adopt the tool. It often finds old secrets.

When it finds a real key: **revoke and rotate it first**. Rewriting history (`git filter-repo`) afterwards is fine for tidiness, but forks, clones and caches already have the old commits.

Clean up when you are done:

```sh
docker rm -f tutor-vault tutor-vault-pg
docker network rm tutor-secrets
docker rmi tutor-leaky:dev
```

## Traps

- **Dev mode in production.** `vault server -dev` keeps everything in memory, unsealed, with a root token. It is for learning only. A real Vault needs storage, TLS, unseal keys or auto-unseal, and audit logging.
- **Forgetting `data/` in KV v2 policies.** `path "secret/orders/*"` matches nothing in KV v2. The API path is `secret/data/orders/*`.
- **Long-lived root tokens.** After setup, revoke the root token. Create a new one only for emergencies (`vault operator generate-root`), with several people present.
- **The secret in the env, again.** `docker inspect`, `/proc/<pid>/environ` and crash reporters see environment variables. Use files or fetch at start-up.
- **`rm` in a Dockerfile.** The layer before still has the file; `ARG` values are in `docker history`. Use build secrets.
- **Deleting instead of revoking.** A leaked key removed from Git still works. Revoke first.
- **Rotation with a gap.** One valid key at a time means downtime during rotation. Support two.
- **Caching a dynamic credential forever.** A service that reads the password once and never renews breaks when the lease ends. Renew at about two thirds of the TTL, and test what happens when Vault is down for a minute.
- **Encrypting with SOPS and thinking you're done.** Everyone who could decrypt the file still knows the values. Rotate the secrets, not just the master key, when people leave.

## Check yourself

??? question "1. A developer committed an API key, noticed after an hour, and pushed a commit that removes it. The repo is private. What do you do?"

    Revoke the key at the provider and create a new one, now. The old commit is in every
    clone, in CI caches, maybe in forks and in the Git server's backups; "private" only
    limits who can read those. Then add a scanner in CI (and push protection on the server)
    so the next one is caught before it lands. Rewriting history is optional and comes last.

??? question "2. Why is `ordersd` with a Vault dynamic database credential safer than with a static password from Vault KV?"

    The dynamic credential is a new Postgres user for this instance, valid for minutes or
    hours, and Vault drops it when the lease ends or on `lease revoke`. A leaked copy soon
    opens nothing, each instance has its own user (so the database audit log says who did
    what), and rotation happens on every lease without human work. A static password
    lives until someone rotates it by hand, and everyone who ever read it can still use it.

??? question "3. Your policy says `path \"secret/orders/*\" { capabilities = [\"read\"] }`, but `vault kv get secret/orders/db` returns 403. Why?"

    The secret engine is KV version 2. The CLI path `secret/orders/db` maps to the API path
    `secret/data/orders/db`, and policies match API paths. The policy should be
    `path "secret/data/orders/*"`. (Use `secret/metadata/orders/*` for listing and versions.)

??? question "4. What is \"secret zero\", and how does workload identity solve it?"

    Every secret store needs a first credential to log in with, and storing that one is
    the same problem again. Workload identity uses something the platform already proves:
    a Kubernetes service account token signed by the API server, a cloud instance identity,
    or a CI OIDC token. The workload shows that token, and the store (Vault, cloud IAM)
    checks its signature, issuer and audience and returns a short-lived credential. No
    stored password at all.

??? question "5. A Dockerfile does `COPY id_deploy /root/.ssh/` and then `RUN git clone ... && rm /root/.ssh/id_deploy`. Is the key in the image?"

    Yes. The `COPY` creates a layer that contains the file. The later `rm` only adds a
    "whiteout" in a newer layer, so the final filesystem hides it, but `docker save` and
    any registry pull deliver every layer. Use `RUN --mount=type=ssh` or
    `--mount=type=secret` with BuildKit, which never writes the file to a layer, or do the
    clone outside the image build.

??? question "6. You must rotate the signing key for session cookies without logging everyone out. How?"

    Make the verifier accept two keys. Add the new key as "verify only", deploy, then
    switch signing to the new key while still accepting the old one. Wait until every
    session signed with the old key has expired (the session lifetime), check that no
    valid old-key sessions remain, then remove the old key. Key IDs in the token or
    cookie (`kid`) make choosing the right key cheap.

## Recap

- A **secret** gives access just by being known. Have fewer of them, make them short-lived, keep them in **one store**, and make rotation routine.
- Secrets leak through **Git history, logs, environment variables, image layers and CI**. `rm` and "delete the commit" don't undo a leak. **Revoke** does.
- **Vault**: everything is a path; **policies** deny by default; **KV v2** keeps versions (and needs `data/` in policies).
- **Dynamic secrets** create a new database user per request with a **lease**. It expires on its own, can be renewed up to `max_ttl`, and `lease revoke -prefix` kills them all in an incident. `rotate-root` removes the last human-known password.
- **Workload identity** (Kubernetes service account tokens, cloud identities, CI OIDC) replaces "secret zero" with a signed, short-lived proof of who the workload is.
- **SOPS** encrypts values in config files with a data key, wrapped by KMS, Vault transit, PGP or age. A MAC covers the whole file. Access to the master key is the real access control.
- **Rotation** needs two valid secrets at once, clients that reload, and a check that nobody uses the old one before you revoke it.
- **Scanners** read history, not just the working tree. Run them in CI and as push protection; a pre-commit hook alone is optional.

Next: [Supply chain security](supply-chain.md), where the attacker doesn't steal your secrets directly, but ships code that runs inside your build.
