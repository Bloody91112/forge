#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$ROOT_DIR/.env"
TEMP_FILE=""

cd "$ROOT_DIR"
umask 077

# --------------------------------------------------
# Common
# --------------------------------------------------

log() {
    printf '\n==> %s\n' "$1"
}

die() {
    printf '\nError: %s\n' "$1" >&2
    exit 1
}

cleanup() {
    if [[ -n "$TEMP_FILE" && -f "$TEMP_FILE" ]]; then
        rm -f -- "$TEMP_FILE"
    fi
}

trap cleanup EXIT

check_requirements() {
    command -v docker >/dev/null 2>&1 ||
        die "Docker is not installed"

    command -v curl >/dev/null 2>&1 ||
        die "curl is not installed"

    docker compose version >/dev/null 2>&1 ||
        die "Docker Compose is unavailable"

    docker info >/dev/null 2>&1 ||
        die "Docker Engine is not running"
}

# --------------------------------------------------
# Environment
# --------------------------------------------------

create_env_file() {
    if [[ -f "$ENV_FILE" ]]; then
        return
    fi

    [[ -f "$ROOT_DIR/.env.example" ]] ||
        die ".env.example not found"

    cp "$ROOT_DIR/.env.example" "$ENV_FILE"
    chmod 600 "$ENV_FILE"

    log "Created .env from .env.example"
}

get_env_value() {
    local key="$1"
    local line

    while IFS= read -r line || [[ -n "$line" ]]; do
        if [[ "$line" == "$key="* ]]; then
            printf '%s' "${line#*=}"
            return 0
        fi
    done < "$ENV_FILE"

    return 0
}

set_env_value() {
    local key="$1"
    local value="$2"
    local line
    local found=0

    TEMP_FILE="$(mktemp "${ENV_FILE}.XXXXXX")"

    {
        while IFS= read -r line || [[ -n "$line" ]]; do
            if [[ "$line" == "$key="* ]]; then
                if (( found == 0 )); then
                    printf '%s=%s\n' "$key" "$value"
                    found=1
                fi
            else
                printf '%s\n' "$line"
            fi
        done < "$ENV_FILE"

        if (( found == 0 )); then
            printf '%s=%s\n' "$key" "$value"
        fi
    } > "$TEMP_FILE"

    chmod 600 "$TEMP_FILE"
    mv "$TEMP_FILE" "$ENV_FILE"

    TEMP_FILE=""
}

validate_env() {
    local required_vars=(
        DB_DATABASE
        DB_TEST_DATABASE
        DB_USERNAME
        DB_PASSWORD
        WORKER_CAPABILITIES
    )

    local name
    local value
    local db_database
    local db_test_database
    local db_port

    for name in "${required_vars[@]}"; do
        value="$(get_env_value "$name")"

        [[ -n "$value" ]] ||
            die "Environment variable $name is missing or empty"
    done

    db_database="$(get_env_value DB_DATABASE)"
    db_test_database="$(get_env_value DB_TEST_DATABASE)"

    [[ "$db_database" != "$db_test_database" ]] ||
        die "DB_DATABASE and DB_TEST_DATABASE must be different"

    db_port="$(get_env_value DB_PORT)"

    [[ -n "$db_port" ]] || db_port=5432

    [[ "$db_port" =~ ^[0-9]+$ ]] ||
        die "DB_PORT must be a number"

    (( db_port >= 1 && db_port <= 65535 )) ||
        die "DB_PORT must be between 1 and 65535"
}

# --------------------------------------------------
# Docker
# --------------------------------------------------

build_images() {
    log "Building Forge images"

    docker compose build forge worker
}

start_postgres() {
    log "Starting PostgreSQL"

    docker compose up -d --wait postgres
}

run_migrations() {
    log "Applying database migrations"

    docker compose --profile manual run --rm migrate
    docker compose --profile manual run --rm migrate-test
}

# --------------------------------------------------
# Bootstrap
# --------------------------------------------------

run_bootstrap() {
    local action="$1"
    local output

    if ! output="$(
        docker compose run --rm --no-deps forge bootstrap "$action" 2>&1
    )"; then
        printf '%s\n' "$output" >&2
        return 1
    fi

    if [[ "$output" =~ Bootstrap\ token:\ ([[:xdigit:]]{64}) ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
    fi
}

initialize_bootstrap() {
    local token

    log "Initializing bootstrap token"

    if ! token="$(run_bootstrap init)"; then
        die "Bootstrap initialization failed"
    fi

    # A new token was generated.
    if [[ "$token" =~ ^[[:xdigit:]]{64}$ ]]; then
        set_env_value WORKER_BOOTSTRAP_TOKEN "$token"
        return
    fi

    # The token already exists in PostgreSQL.
    # Keep the configured token if it is present.
    token="$(get_env_value WORKER_BOOTSTRAP_TOKEN)"

    if [[ "$token" =~ ^[[:xdigit:]]{64}$ ]]; then
        return
    fi

    # The token hash exists, but its plaintext value is unavailable.
    log "Bootstrap token is missing from .env; rotating it"

    if ! token="$(run_bootstrap rotate)"; then
        die "Bootstrap token rotation failed"
    fi

    [[ "$token" =~ ^[[:xdigit:]]{64}$ ]] ||
        die "Failed to obtain bootstrap token"

    set_env_value WORKER_BOOTSTRAP_TOKEN "$token"
}

# --------------------------------------------------
# Forge
# --------------------------------------------------

start_forge() {
    log "Starting Forge API"

    docker compose up -d forge
}

wait_for_forge() {
    local attempts=30

    log "Waiting for Forge API readiness"

    for ((i = 1; i <= attempts; i++)); do
        if curl \
            --fail \
            --silent \
            --connect-timeout 1 \
            --max-time 2 \
            http://127.0.0.1:8080/readyz >/dev/null; then
            return 0
        fi

        sleep 1
    done

    docker compose logs forge

    die "Forge API did not become ready"
}

start_worker() {
    log "Starting Worker"

    docker compose up -d worker
}

wait_for_worker() {
    local attempts=30
    local active_workers=""

    log "Waiting for Worker registration"

    for ((i = 1; i <= attempts; i++)); do
        if active_workers="$(
            docker compose exec -T postgres sh -c \
                'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "SELECT COUNT(*) FROM workers WHERE lease_expires_at > clock_timestamp()"' \
                2>/dev/null
        )"; then

            active_workers="${active_workers//[[:space:]]/}"

            if [[ "$active_workers" =~ ^[1-9][0-9]*$ ]]; then
                return 0
            fi
        fi

        sleep 1
    done

    docker compose logs worker

    die "No active Worker registered with Forge"
}

# --------------------------------------------------
# Main
# --------------------------------------------------

main() {
    check_requirements

    create_env_file
    chmod 600 "$ENV_FILE"

    validate_env

    docker compose config --quiet ||
        die "Invalid Docker Compose configuration"

    build_images
    start_postgres
    run_migrations
    initialize_bootstrap

    start_forge
    wait_for_forge

    start_worker
    wait_for_worker

    log "Forge environment is ready"

    printf 'API: http://localhost:8080\n'
}

main "$@"
