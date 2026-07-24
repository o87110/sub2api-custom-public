#!/usr/bin/env bash

release_manifest_action() {
  local release_state="$1"
  local manifest_present="$2"
  local current_artifacts_present="$3"

  case "$release_state:$manifest_present:$current_artifacts_present" in
    published:true:true | published:true:false | draft:true:true | draft:true:false)
      printf '%s\n' remote-manifest
      ;;
    draft:false:true | draft:false:false | remote:false:true | remote:false:false)
      printf '%s\n' recover-manifest
      ;;
    none:false:true)
      printf '%s\n' current-manifest
      ;;
    none:false:false)
      echo "new Release state requires current-run payload and manifest artifacts" >&2
      return 1
      ;;
    published:false:true | published:false:false)
      echo "published Release is missing its authoritative manifest" >&2
      return 1
      ;;
    *)
      echo "inconsistent Release/manifest state: $release_state/$manifest_present" >&2
      return 1
      ;;
  esac
}

select_consistent_manifest_candidate() {
  local candidates="$1"
  local unique_candidates

  [[ -s "$candidates" ]] || {
    echo "no valid manifest artifact can recover the existing remote publishing state" >&2
    return 1
  }
  awk -F '\t' '
    NF != 4 || $1 == "" || $2 == "" || $3 == "" || $4 == "" { exit 1 }
  ' "$candidates" || {
    echo "manifest candidate set is malformed" >&2
    return 1
  }
  unique_candidates="$(
    cut -f1,2 "$candidates" |
      LC_ALL=C sort -u |
      wc -l |
      tr -d ' '
  )"
  [[ "$unique_candidates" -eq 1 ]] || {
    echo "conflicting manifest artifacts prevent unique recovery" >&2
    return 1
  }
  awk -F '\t' 'NR == 1 { print $4; exit }' "$candidates"
}

draft_assets_follow_manifest() {
  local release_json="$1"

  jq -e '
    [.assets[] | select(.name == "release-manifest.json")] as $manifests |
    if ($manifests | length) == 0 then
      true
    elif ($manifests | length) == 1 then
      ($manifests[0].created_at | fromdateiso8601) as $manifest_created_at |
      all(
        .assets[];
        .name == "release-manifest.json" or
        ((.created_at | fromdateiso8601) >= $manifest_created_at)
      )
    else
      false
    end
  ' <<<"$release_json" >/dev/null
}
