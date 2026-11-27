class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.14"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.14/xissue-v0.8.14-darwin-arm64.tar.gz"
      sha256 "03d89a83f768e08f6767fa44319e6a22dcef16d1b38fdcfa14e1c5fd572fa2a7"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.14/xissue-v0.8.14-darwin-amd64.tar.gz"
      sha256 "baa04e243dcab9d1e9f3e86d840d206ed5d74473fc1a58238c6d4b86f167ee9f"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.14/xissue-v0.8.14-linux-arm64.tar.gz"
      sha256 "380ebb40f4c67c7b1ce62a68c65fd8a7c3622c4125a33105956bc707babfc929"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.14/xissue-v0.8.14-linux-amd64.tar.gz"
      sha256 "1cf8dec144ff34bdceb367a8e08b0d1084c185cf4adb344ca33dbcc840b4e2b7"
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
