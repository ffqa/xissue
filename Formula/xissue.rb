class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.9"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.9/xissue-v0.8.9-darwin-arm64.tar.gz"
      sha256 "50310782fad065db883e0f4c582abcf32bad9b30e94138d37bf8aace53519bb0"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.9/xissue-v0.8.9-darwin-amd64.tar.gz"
      sha256 "27434ffa708fc2334e94ec3a6a7a68e84fdf3bedd4964ca270f522a84e7d94bc"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.9/xissue-v0.8.9-linux-arm64.tar.gz"
      sha256 "747a8df00e520f577e76f91dc0911afe6d1948a3a780a850ec9b3d08ac2171c5"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.9/xissue-v0.8.9-linux-amd64.tar.gz"
      sha256 "f6b73280f161dc956122ed466e959eccb0fefaa5a3a1c5450802af94196ac1ac"
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
