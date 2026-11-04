class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.6"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.6/xissue-v0.8.6-darwin-arm64.tar.gz"
      sha256 "304f9ff8495a3b7331cdea8a2f1afc6d3baac19d9b5ad55034bd0bdae6d28633"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.6/xissue-v0.8.6-darwin-amd64.tar.gz"
      sha256 "343934a8529643e2078d717564c3568d8c2c804ae144d367e3a127f145070af3"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.6/xissue-v0.8.6-linux-arm64.tar.gz"
      sha256 "d71693836832a71cf72584f07df061809c15450e53848146b309aa46e4567cc4"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.6/xissue-v0.8.6-linux-amd64.tar.gz"
      sha256 "95777ff608f702839ffd772d7642f0cb14521c25b2dc2fcfb53b2fd741b272f3"
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
