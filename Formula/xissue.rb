class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.13"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.13/xissue-v0.5.13-darwin-arm64.tar.gz"
      sha256 "82a819279f177900313e731f732d20f9e27acf21b0fd9e4a99d93b5495fca1d5"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.13/xissue-v0.5.13-darwin-amd64.tar.gz"
      sha256 "cbb1f18d2bd266ed2bbdd964dabdde38c0d85ec91310e9980f4782b74f40f6be"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.13/xissue-v0.5.13-linux-arm64.tar.gz"
      sha256 "f87dc37f5521a7e7e4d3e072b000c24c5ea701bcadf0f5510fe081217842abf0"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.13/xissue-v0.5.13-linux-amd64.tar.gz"
      sha256 "0875d955cddcbd5b8372a591ce2f6d47e47d7598d34a4d6ad7b1a46bd813ccdc"
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
