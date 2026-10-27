class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.36"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.36/xissue-v0.2.36-darwin-arm64.tar.gz"
      sha256 "136c00def2b4a52bf19b27d8ffa6b0fc9e67d6f599d645b34e078a490e425bad"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.36/xissue-v0.2.36-darwin-amd64.tar.gz"
      sha256 "429e92a5e9c8bcc6f52e3f88dae7960143c6f5c21f351f2795a30e097668d882"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.36/xissue-v0.2.36-linux-arm64.tar.gz"
      sha256 "31ea4770f7741804fdc5891fe0ac4cbba04e9b4a5452e3130368d3d160d8fc1a"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.36/xissue-v0.2.36-linux-amd64.tar.gz"
      sha256 "cdb87281e1bf1440eec1bc6bb5f82331078bad0155e4c36a0f9cd4baaa5d9b56"
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
