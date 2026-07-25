#!/bin/sh
set -e

IMAGE_BINARY=${IMAGE_BINARY:-/app/image/sub2api}
LEGACY_IMAGE_BINARY=${LEGACY_IMAGE_BINARY:-/app/sub2api}
RUNTIME_BINARY=${RUNTIME_BINARY:-/app/runtime/sub2api}
RUNTIME_DIR=${RUNTIME_DIR:-$(dirname "$RUNTIME_BINARY")}
RUNTIME_SECRET_DIR=${RUNTIME_SECRET_DIR:-/run/sub2api-secrets}
RUNTIME_UPDATE_TOKEN=$RUNTIME_SECRET_DIR/update-token
BINARY_VERSION_TIMEOUT_SECONDS=${BINARY_VERSION_TIMEOUT_SECONDS:-5}
BINARY_VERSION_MAX_OUTPUT_BYTES=${BINARY_VERSION_MAX_OUTPUT_BYTES:-8192}

reject_symlink() {
    managed_path=$1
    managed_kind=$2
    if [ -L "$managed_path" ]; then
        echo "ERROR: $managed_kind path must not be a symbolic link: $managed_path" >&2
        return 1
    fi
}

reject_managed_binary_symlinks() {
    reject_symlink "$IMAGE_BINARY" "image binary" || return 1
    reject_symlink "$LEGACY_IMAGE_BINARY" "legacy image binary" || return 1
    reject_symlink "$RUNTIME_BINARY" "runtime binary" || return 1
    reject_symlink "$RUNTIME_BINARY.backup" "runtime backup" || return 1
}

resolve_image_binary() {
    reject_symlink "$IMAGE_BINARY" "image binary" || return 1
    reject_symlink "$LEGACY_IMAGE_BINARY" "legacy image binary" || return 1
    if [ "$IMAGE_BINARY" = "/app/image/sub2api" ] &&
        [ ! -e "$IMAGE_BINARY" ] &&
        [ -e "$LEGACY_IMAGE_BINARY" ]; then
        IMAGE_BINARY=$LEGACY_IMAGE_BINARY
    fi
    export IMAGE_BINARY
}

validate_binary() {
    binary_path=$1
    binary_kind=$2
    reject_symlink "$binary_path" "$binary_kind binary" || return 1
    if [ ! -f "$binary_path" ]; then
        echo "ERROR: $binary_kind binary is not a regular file: $binary_path" >&2
        return 1
    fi
    if [ ! -s "$binary_path" ]; then
        echo "ERROR: $binary_kind binary is empty: $binary_path" >&2
        return 1
    fi
    if [ ! -x "$binary_path" ]; then
        echo "ERROR: $binary_kind binary is not executable: $binary_path" >&2
        return 1
    fi
    magic="$(od -An -tx1 -N4 "$binary_path" 2>/dev/null | tr -d ' \n')"
    if [ "$magic" != "7f454c46" ]; then
        echo "ERROR: $binary_kind binary is not a valid ELF executable: $binary_path" >&2
        return 1
    fi
}

binary_version() {
    binary_path=$1
    binary_kind=$2
    validate_binary "$binary_path" "$binary_kind" || return 1

    version_output_file="$(mktemp "${TMPDIR:-/tmp}/sub2api-version.XXXXXX")" || {
        echo "ERROR: failed to allocate $binary_kind version output file" >&2
        return 1
    }
    output_file_blocks=$(((BINARY_VERSION_MAX_OUTPUT_BYTES + 511) / 512))
    set +e
    (
        ulimit -f "$output_file_blocks" 2>/dev/null || exit 1
        exec timeout -s TERM -k 1 "$BINARY_VERSION_TIMEOUT_SECONDS" "$binary_path" --version
    ) >"$version_output_file" 2>&1
    probe_status=$?
    set -e

    version_output_size="$(wc -c <"$version_output_file" | tr -d ' ')"
    if [ "$probe_status" -ne 0 ] || [ "$version_output_size" -gt "$BINARY_VERSION_MAX_OUTPUT_BYTES" ]; then
        rm -f "$version_output_file"
        echo "ERROR: failed to read $binary_kind binary version: $binary_path" >&2
        return 1
    fi
    version_output="$(cat "$version_output_file")"
    rm -f "$version_output_file"

    version="$(
        printf '%s\n' "$version_output" |
            sed -nE 's/^.*Sub2API[[:space:]]+(v?[0-9]+\.[0-9]+\.[0-9]+(-custom\.[0-9]+)?)([[:space:]].*)?$/\1/p' |
            sed -n '1p'
    )"
    if [ -z "$version" ]; then
        echo "ERROR: $binary_kind binary returned an unsupported version: $version_output" >&2
        return 1
    fi
    printf '%s\n' "${version#v}"
}

version_parts() {
    printf '%s\n' "$1" |
        sed -nE 's/^([0-9]+)\.([0-9]+)\.([0-9]+)(-custom\.([0-9]+))?$/\1 \2 \3 \5/p'
}

is_custom_version() {
    printf '%s\n' "$1" |
        grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+$'
}

compare_versions() {
    left_parts="$(version_parts "$1")"
    right_parts="$(version_parts "$2")"
    [ -n "$left_parts" ] && [ -n "$right_parts" ] || return 1

    set -- $left_parts
    left_major=$1
    left_minor=$2
    left_patch=$3
    left_custom=${4:-0}
    set -- $right_parts
    right_major=$1
    right_minor=$2
    right_patch=$3
    right_custom=${4:-0}

    if [ "$left_major" -gt "$right_major" ]; then echo 1; return; fi
    if [ "$left_major" -lt "$right_major" ]; then echo -1; return; fi
    if [ "$left_minor" -gt "$right_minor" ]; then echo 1; return; fi
    if [ "$left_minor" -lt "$right_minor" ]; then echo -1; return; fi
    if [ "$left_patch" -gt "$right_patch" ]; then echo 1; return; fi
    if [ "$left_patch" -lt "$right_patch" ]; then echo -1; return; fi
    if [ "$left_custom" -gt "$right_custom" ]; then echo 1; return; fi
    if [ "$left_custom" -lt "$right_custom" ]; then echo -1; return; fi
    echo 0
}

install_image_binary() {
    image_version=$1
    keep_backup=$2
    temp_binary="$RUNTIME_BINARY.image.$$"
    temp_backup="$RUNTIME_BINARY.backup.$$"
    backup_binary="$RUNTIME_BINARY.backup"
    previous_backup="$RUNTIME_BINARY.backup.previous.$$"
    reject_managed_binary_symlinks || return 1
    reject_symlink "$temp_binary" "staged image binary" || return 1
    reject_symlink "$temp_backup" "staged runtime backup" || return 1
    reject_symlink "$previous_backup" "previous runtime backup" || return 1
    rm -f "$temp_binary" "$temp_backup"
    if [ -e "$previous_backup" ] || [ -L "$previous_backup" ]; then
        echo "ERROR: previous backup staging path already exists: $previous_backup" >&2
        return 1
    fi

    reject_symlink "$IMAGE_BINARY" "image binary" || return 1
    if ! cp "$IMAGE_BINARY" "$temp_binary" ||
        ! chmod 0755 "$temp_binary" ||
        [ "$(binary_version "$temp_binary" "staged image")" != "$image_version" ]; then
        rm -f "$temp_binary" "$temp_backup"
        echo "ERROR: failed to stage image binary for atomic installation" >&2
        return 1
    fi

    if [ "$keep_backup" = "true" ]; then
        reject_symlink "$RUNTIME_BINARY" "runtime binary" || return 1
        reject_symlink "$backup_binary" "runtime backup" || return 1
        if ! cp "$RUNTIME_BINARY" "$temp_backup" ||
            ! chmod 0755 "$temp_backup"; then
            rm -f "$temp_binary" "$temp_backup"
            echo "ERROR: failed to preserve the previous runtime binary" >&2
            return 1
        fi
        reject_symlink "$backup_binary" "runtime backup" || return 1
        reject_symlink "$previous_backup" "previous runtime backup" || return 1
        if [ -e "$backup_binary" ] && ! mv "$backup_binary" "$previous_backup"; then
            rm -f "$temp_binary" "$temp_backup"
            echo "ERROR: failed to preserve the existing runtime backup" >&2
            return 1
        fi
        reject_symlink "$temp_backup" "staged runtime backup" || return 1
        reject_symlink "$backup_binary" "runtime backup" || return 1
        if ! mv "$temp_backup" "$backup_binary"; then
            restore_error=
            if [ -e "$previous_backup" ] || [ -L "$previous_backup" ]; then
                reject_symlink "$previous_backup" "previous runtime backup" || return 1
                if ! mv "$previous_backup" "$backup_binary"; then
                    restore_error="; previous backup remains at $previous_backup"
                fi
            fi
            rm -f "$temp_binary" "$temp_backup"
            echo "ERROR: failed to promote the current runtime backup$restore_error" >&2
            return 1
        fi
    fi

    reject_symlink "$temp_binary" "staged image binary" || return 1
    reject_symlink "$RUNTIME_BINARY" "runtime binary" || return 1
    if ! mv "$temp_binary" "$RUNTIME_BINARY"; then
        restore_error=
        if [ "$keep_backup" = "true" ]; then
            reject_symlink "$backup_binary" "runtime backup" || return 1
            rm -f "$backup_binary"
            if [ -e "$previous_backup" ] || [ -L "$previous_backup" ]; then
                reject_symlink "$previous_backup" "previous runtime backup" || return 1
                if ! mv "$previous_backup" "$backup_binary"; then
                    restore_error="; previous backup remains at $previous_backup"
                fi
            fi
        fi
        rm -f "$temp_binary" "$temp_backup"
        echo "ERROR: failed to install the image binary$restore_error" >&2
        return 1
    fi

    if [ "$keep_backup" = "true" ]; then
        reject_symlink "$previous_backup" "previous runtime backup" || return 1
        rm -f "$previous_backup"
    else
        reject_symlink "$backup_binary" "runtime backup" || return 1
    fi
    if [ "$keep_backup" != "true" ] && ! rm -f "$backup_binary"; then
        echo "ERROR: installed trusted image binary but failed to remove stale runtime backup" >&2
        return 1
    fi
}

reconcile_runtime_binary() {
    reject_managed_binary_symlinks || return 1
    image_version="$(binary_version "$IMAGE_BINARY" "image")" || return 1
    if [ ! -e "$RUNTIME_BINARY" ]; then
        install_image_binary "$image_version" false
        echo "Initialized runtime binary from image version $image_version."
        return
    fi

    runtime_version="$(binary_version "$RUNTIME_BINARY" "runtime")" || return 1
    if is_custom_version "$image_version" && ! is_custom_version "$runtime_version"; then
        install_image_binary "$image_version" false
        echo "Replaced non-custom runtime version $runtime_version with image version $image_version."
        return
    fi
    comparison="$(compare_versions "$image_version" "$runtime_version")" || {
        echo "ERROR: failed to compare image and runtime versions" >&2
        return 1
    }
    case "$comparison" in
        1)
            install_image_binary "$image_version" true
            echo "Upgraded runtime binary from $runtime_version to image version $image_version."
            ;;
        0)
            echo "Runtime binary already matches image version $image_version."
            ;;
        -1)
            echo "Keeping newer runtime version $runtime_version over image version $image_version."
            ;;
        *)
            echo "ERROR: unexpected version comparison result: $comparison" >&2
            return 1
            ;;
    esac
}

prepare_update_token_file() {
    source_token=${UPDATE_GITHUB_TOKEN_FILE:-}
    if [ -z "$source_token" ] || [ ! -f "$source_token" ]; then
        return
    fi

    temp_token="$RUNTIME_UPDATE_TOKEN.$$"
    if ! mkdir -p "$RUNTIME_SECRET_DIR" ||
        ! chown sub2api:sub2api "$RUNTIME_SECRET_DIR" ||
        ! chmod 0700 "$RUNTIME_SECRET_DIR" ||
        ! rm -f "$temp_token" "$RUNTIME_UPDATE_TOKEN" ||
        ! cp "$source_token" "$temp_token" ||
        ! chown sub2api:sub2api "$temp_token" ||
        ! chmod 0400 "$temp_token" ||
        ! mv "$temp_token" "$RUNTIME_UPDATE_TOKEN"; then
        rm -f "$temp_token" "$RUNTIME_UPDATE_TOKEN" || true
        echo "WARNING: failed to prepare the optional GitHub Token file" >&2
        return
    fi

    export UPDATE_GITHUB_TOKEN_FILE=$RUNTIME_UPDATE_TOKEN
}

main() {
    resolve_image_binary

    # Root only prepares ownership and secrets. Version probing happens after
    # dropping privileges because the persistent runtime is application-writable.
    if [ "$(id -u)" = "0" ]; then
        mkdir -p /app/data "$RUNTIME_DIR"
        chown -R sub2api:sub2api /app/data 2>/dev/null || true
        chown sub2api:sub2api "$RUNTIME_DIR"
        prepare_update_token_file
        exec su-exec sub2api "$0" "$@"
    fi

    reconcile_runtime_binary

    # Preserve the old ENTRYPOINT/CMD flag behavior while running the selected
    # persistent binary for default and flag-only invocations.
    if [ "$#" -eq 0 ]; then
        set -- "$RUNTIME_BINARY"
    elif [ "$1" = "/app/sub2api" ] ||
        [ "$1" = "/app/image/sub2api" ] ||
        [ "$1" = "$IMAGE_BINARY" ]; then
        shift
        set -- "$RUNTIME_BINARY" "$@"
    elif [ "${1#-}" != "$1" ]; then
        set -- "$RUNTIME_BINARY" "$@"
    fi

    if [ "$1" = "$RUNTIME_BINARY" ]; then
        reject_symlink "$RUNTIME_BINARY" "runtime binary" || return 1
    fi
    exec "$@"
}

if [ "${ENTRYPOINT_SOURCE_ONLY:-false}" != "true" ]; then
    main "$@"
fi
