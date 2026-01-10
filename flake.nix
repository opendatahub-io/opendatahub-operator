{
  description = "Dev shell with Go 1.24 and operator-sdk v1.37.0";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }:
    let
      system = "aarch64-darwin"; # M1/M2 Mac
      pkgs = import nixpkgs { inherit system; };

      # 1. Define your Environment Variables here (Single Source of Truth)
      myEnv = {
        IMAGE_TAG_BASE = "quay.io/rh-ee-lbondy/opendatahub-operator";
        OPERATOR_NAMESPACE = "opendatahub-operator-system";
        # Uncomment these if you want them active:
        # PLATFORM = "linux/arm64";
        # CGO_ENABLED = "0";
      };

      # 2. Custom Packages
      operatorSdk = pkgs.stdenv.mkDerivation {
        pname = "operator-sdk";
        version = "1.37.0";
        src = pkgs.fetchurl {
          url = "https://github.com/operator-framework/operator-sdk/releases/download/v1.37.0/operator-sdk_darwin_arm64";
          sha256 = "sha256-KljNEIZWVZN8OimDaLRTeZN+GL4gX4y0KaShxRpfkq8=";
        };
        phases = [ "installPhase" ];
        installPhase = ''
          mkdir -p $out/bin
          cp $src $out/bin/operator-sdk
          chmod +x $out/bin/operator-sdk
        '';
      };

      gsedAlias = pkgs.runCommand "gsed-alias" {} ''
        mkdir -p $out/bin
        ln -s ${pkgs.gnused}/bin/sed $out/bin/gsed
      '';

      ompNixDevEnvsParser = pkgs.writeShellScriptBin "omp-nix-dev-envs-parser" ''
        if [ -n "$FLAKE_CONFIG_JSON" ]; then
          echo "$FLAKE_CONFIG_JSON" | ${pkgs.jq}/bin/jq -r '
            to_entries | 
            map(
              # LOGIC:
              # 1. <cyan>\uE0B6</>        -> Left Cap (Cyan Color)
              # 2. <black,cyan> ... </>   -> The Body (Black Text on Cyan Background)
              # 3. <cyan>\uE0B4</>        -> Right Cap (Cyan Color)
              
              "<cyan>\uE0B6</><black,cyan> \(.key)=\(.value) </><cyan>\uE0B4</>"
            ) | 
            join(" ")'
        fi
      '';

    in
    {
      devShells.${system}.default = pkgs.mkShell {
        buildInputs = [ pkgs.go_1_24 operatorSdk pkgs.gnused gsedAlias ompNixDevEnvsParser ];

        # 3. Pass Env Vars to the Shell (Direnv/Nix Develop will load these)
        env = myEnv // {
	  FLAKE_CONFIG_JSON = builtins.toJSON myEnv;
	};

        # 4. Expose Env Vars for inspection (nix eval .#...configEnv)
        passthru = {
          configEnv = myEnv;
        };

        # 5. ShellHook is now purely for printing info, not logic
        shellHook = ''
          echo "Go version: $(go version)"
          echo "operator-sdk version: $(operator-sdk version)"
          echo "gsed is available at: $(which gsed)"
        '';
      };
    };
}
