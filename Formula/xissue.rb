class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.33"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.33/xissue-v0.2.33-darwin-arm64.tar.gz"
      sha256 "c13513006735221582d1f6e38fc7233cb990f7b211fed4874c31c8121cf27b54"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.33/xissue-v0.2.33-darwin-amd64.tar.gz"
      sha256 "e1ce015d5ece2b9e24a04737c8275655c075c9fb489a6467debd36cbf3661126"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.33/xissue-v0.2.33-linux-arm64.tar.gz"
      sha256 "b01656e2be5ce7fa73dd88a85b94f5f3da913e9762267d9e11a83f333b33d0a1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.33/xissue-v0.2.33-linux-amd64.tar.gz"
      sha256 "1bc7a0b76ecd9593fb1009c050844eed0281af28e8ebb93e2ee5f8de2d5076bd"
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
    environment_variables XISSUE_SERVICE: "1"
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  def caveats
    <<~EOS
      启动后台常驻服务方式（二选一）：
      1. Homebrew 服务管理:
         brew services start ffqa/tap/xissue
      2. xIssue 自管服务 (推荐，开机自启):
         xissue service install
    EOS
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
