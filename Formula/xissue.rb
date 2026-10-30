class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.15"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.15/xissue-v0.5.15-darwin-arm64.tar.gz"
      sha256 "7d9a06f54d349e7321ef24b3594e3d03a2f8ac93e76bfc1580dceafffdb1fa4e"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.15/xissue-v0.5.15-darwin-amd64.tar.gz"
      sha256 "bbc204a65f1b00fd9c88b951f00c04b81871aacc0ef1ed7bc8fd696b26775f97"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.15/xissue-v0.5.15-linux-arm64.tar.gz"
      sha256 "b7956f4e3867a38918d41d06bd8b4930dcc89ff3ad0fe76793725583fc7e7a82"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.15/xissue-v0.5.15-linux-amd64.tar.gz"
      sha256 "484c6feaab13ee25ba3fe6f84488c258d3b680af639dbe2b1c40be14a6c206c6"
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
