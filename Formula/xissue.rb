class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.12"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.12/xissue-v0.8.12-darwin-arm64.tar.gz"
      sha256 "a6d6f1e6aeffc9d37713591069032fe0aceb6b07cd494d9fc297d0e9f49f37a7"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.12/xissue-v0.8.12-darwin-amd64.tar.gz"
      sha256 "865fa6db1c694e4543f2d5e07dbd08606df51d1f85f049e9e64da6a45a9a1b30"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.12/xissue-v0.8.12-linux-arm64.tar.gz"
      sha256 "56f3395dbdbdb9d3d8ec57612bd521de23b250adcdb1c5f94d860dbf51993fad"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.12/xissue-v0.8.12-linux-amd64.tar.gz"
      sha256 "1f80f5863f15a068766729a6354f6e61647503d05c551dc9a1aef5c755450e65"
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
