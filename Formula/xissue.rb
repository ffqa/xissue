class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.42"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.42/xissue-v0.2.42-darwin-arm64.tar.gz"
      sha256 "c5a2e8702583299ea7189fb1aa7a75c5a042cc7b5849aaaf8391e8c86de1b44f"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.42/xissue-v0.2.42-darwin-amd64.tar.gz"
      sha256 "d441efa65b52b71bd788640c56a2f21b0c6c12b83a15faca47721c2a610bca4b"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.42/xissue-v0.2.42-linux-arm64.tar.gz"
      sha256 "7202950b83eeb24536076eefc930c6df21b2992f04ad380c5b2c69cdbfb4dda4"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.42/xissue-v0.2.42-linux-amd64.tar.gz"
      sha256 "a4a38a55697d0b2105cceda20c164de52b5cced72e4086150ef1aae3fb28063e"
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
