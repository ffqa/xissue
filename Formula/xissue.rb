class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.37"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.37/xissue-v0.2.37-darwin-arm64.tar.gz"
      sha256 "1d9acb7ad48b7f87ada053a013844e3e2ef374abb2e64670d4007fb3917b617f"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.37/xissue-v0.2.37-darwin-amd64.tar.gz"
      sha256 "f25db2aac72d9f941c361ef307f9abba7dd73339628589a8afeab1045cc0bc94"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.37/xissue-v0.2.37-linux-arm64.tar.gz"
      sha256 "20b2cd9227b7e0bc554e2338d4deb61d1f2f7799ef22afdb33649642b99588e6"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.37/xissue-v0.2.37-linux-amd64.tar.gz"
      sha256 "9768eeea52392474c44371733e7518b3b2e6f1d0f56516176401f8d24a5be148"
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
