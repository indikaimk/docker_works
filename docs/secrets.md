# Secrets Management (SOPS)

DockerWorks supports decrypting [SOPS](https://github.com/getsops/sops)-encrypted files (`.env`, `.yaml`, `.json`, or binary) committed to Git or bundled in OCI artifacts.

Instead of storing dozens of individual environment variables in AWS Secrets Manager, you can commit all encrypted app secrets in `secrets.enc.env` alongside your Compose/Quadlet files and store only **one** decryption key on the host (or use an AWS KMS IAM role).

## Prerequisites

- Install the `sops` CLI on the host (and `age` on your workstation if using `age` keys).
- `docker-works doctor --config /etc/docker-works/config.yaml` automatically verifies `sops` is on `PATH` when any app configures `sops:`.

---

## Option A: `age` Keypair + Single Key in AWS Secrets Manager / Host Env

1. **Generate an `age` keypair** (once):

   ```bash
   age-keygen -o age.key
   # Public key: age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
   # Private key (inside age.key): AGE-SECRET-KEY-1...
   ```

2. **Add `.sops.yaml` to your app repository** (using the public key):

   ```yaml
   creation_rules:
     - path_regex: \.enc\.(env|yaml|yml|json)$
       age: "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
   ```

3. **Encrypt your secrets file and commit it:**

   ```bash
   sops -e secrets.env > secrets.enc.env
   git add .sops.yaml secrets.enc.env
   git commit -m "chore: add SOPS-encrypted secrets"
   ```

4. **Store the single `AGE-SECRET-KEY-1...` on the host** (e.g. fetched from AWS Secrets Manager into `/etc/docker-works/env` with `chmod 0600`):

   ```ini
   # /etc/docker-works/env
   SOPS_AGE_KEY=AGE-SECRET-KEY-1...
   ```

   Reference it in a systemd drop-in (`sudo systemctl edit docker-works`):

   ```ini
   [Service]
   EnvironmentFile=/etc/docker-works/env
   ```

5. **Configure `apps[].sops` in `config.yaml`:**

   ```yaml
   apps:
     - name: "my-app"
       env_files:
         - "secrets.env"
       sops:
         age_key: "${SOPS_AGE_KEY}"
         files:
           - source: "secrets.enc.env"
             target: "secrets.env"
             output_type: "dotenv"
   ```

---

## Option B: AWS KMS (Zero Key Files)

If your host runs on AWS with an IAM role that has `kms:Decrypt` permission:

1. **Add the KMS ARN to `.sops.yaml`:**

   ```yaml
   creation_rules:
     - path_regex: \.enc\.(env|yaml|yml|json)$
       kms: "arn:aws:kms:us-east-1:123456789012:key/your-key-id"
   ```

2. **Encrypt and commit** (`sops -e secrets.env > secrets.enc.env`).
3. **Omit `age_key` / `age_key_file` in `config.yaml`:**

   ```yaml
   apps:
     - name: "my-app"
       env_files:
         - "secrets.env"
       sops:
         files:
           - source: "secrets.enc.env"
   ```

   `sops --decrypt` automatically uses the host's AWS IAM role credentials.

---

## Using Decrypted Files in Compose & Quadlets

DockerWorks writes decrypted files into `work_dir` with **`0600` permissions** atomically before `pre_deploy` hooks and container startup:

=== "Docker / Podman Compose"

    ```yaml
    apps:
      - name: "my-app"
        env_files:
          - "secrets.env"
        sops:
          age_key: "${SOPS_AGE_KEY}"
          files:
            - source: "secrets.enc.env"
              target: "secrets.env"
    ```

=== "Podman Quadlets"

    Combine `apps[].sops` with [portable `./` paths](podman.md#portable-relative-paths):

    ```ini
    # my-app.container
    [Container]
    Image=ghcr.io/myorg/my-app:latest
    EnvironmentFile=./secrets.env
    Volume=./app-config.yaml:/etc/app/config.yaml:Z,ro
    ```

---

## Rollback Behavior

When a deployment fails health checks and `auto_rollback` triggers, DockerWorks restores the previous Git commit (or OCI artifact digest) **and re-decrypts the previous version's SOPS files** before restarting containers—keeping code, config, and secrets in sync.
