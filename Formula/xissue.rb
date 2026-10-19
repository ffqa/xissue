class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.6"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.6/xissue-v0.2.6-darwin-arm64.tar.gz"
      sha256 "7491bdd26f1c6f39bfe0e8f7160d9bd66050d3e27e06ed01e7211ec43100e314"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.6/xissue-v0.2.6-darwin-amd64.tar.gz"
      sha256 "be8946dad81495ba467f59966b9a39f6d74b4e7c42dc97bf78dab7dc89767a19"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.6/xissue-v0.2.6-linux-arm64.tar.gz"
      sha256 "c340773483fd78f5cd94bbf4282c8ea1eae51e08e791d8f9c80a669293d147de"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.6/xissue-v0.2.6-linux-amd64.tar.gz"
      sha256 "a55bf05dae7fe8c90497de7d3a6fe0be73a7e8c401736754c910d30b819d9f63"
    end
  end

  def install
    bin.install "xissue"
    (pkgshare/"scripts").install Dir["scripts/*"] if Dir.exist?("scripts")
    (pkgshare/"deploy").install Dir["deploy/*"] if Dir.exist?("deploy")
  end

  service do
    run [opt_bin/"xissue"]
    keep_alive true
    working_dir Dir.home
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
