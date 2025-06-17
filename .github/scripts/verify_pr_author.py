import yaml
import os
import sys

def parse_org_and_repo_name(repo_ref):
    parsed_names = repo_ref.split('/')[-2:]
    org_name = parsed_names[0]
    repo_name = parsed_names[1]

    return org_name, repo_name

def verify_pr_author():
    pr_author = os.environ.get('PR_AUTHOR')
    fork_repo = os.environ.get('FORK_REPO')

    fork_org, fork_repo_name = parse_org_and_repo_name(fork_repo)
    try:
        with open('OWNERS_ALIASES', 'r') as f:
            owners_data = yaml.safe_load(f)

        platform_owners = owners_data.get('aliases', {}).get('platform', [])
        if fork_org not in platform_owners:
            print(f"Fork owner not found in OWNERS_ALIASES file. Aborting.")
            sys.exit(1)

        if pr_author in platform_owners:
            print(f"PR author found in OWNERS_ALIASES file. Proceeding.")
            sys.exit(0)
        else:
            print(f"PR author not found in the OWNERS_ALIASES file. Aborting.")
            sys.exit(1)
    except FileNotFoundError:
        print("OWNERS_ALIASES file not found. Aborting.")
        sys.exit(1)
    except Exception as e:
        print(f"Error occurred while parsing OWNERS_ALIASES file: {e}. Aborting.")
        sys.exit(1)

if __name__ == "__main__":
    verify_pr_author()
