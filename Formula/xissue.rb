class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.3"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.3/xissue-v0.8.3-darwin-arm64.tar.gz"
      sha256 "27a4256e2089b6f02ae955840ff177ba7b7139defa3352cb23c8cdab52da5fab"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.3/xissue-v0.8.3-darwin-amd64.tar.gz"
      sha256 "b04ea188ca03c650a32d6ac73392e48ee7490b03ded5323aaa6e97622e82642e"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.3/xissue-v0.8.3-linux-arm64.tar.gz"
      sha256 "eec7e2373f740675a8c7195ce9c1c7f7856c6a502f5d32b7d989df041a377426"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.3/xissue-v0.8.3-linux-amd64.tar.gz"
      sha256 "ed1d649dbfb5d4d8ce11122c3e20d9aca0538dd5a37d1bec560b3d9846639971"
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
