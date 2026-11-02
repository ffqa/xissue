class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.1"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.1/xissue-v0.8.1-darwin-arm64.tar.gz"
      sha256 "0eb1fabb8f6d622bbe1d339d1ec00c92a946f121cd8944a9addce2569fb11276"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.1/xissue-v0.8.1-darwin-amd64.tar.gz"
      sha256 "3cb3ff7e761a12a1220c9f1cdff31a1fae449ec8739327c8c4a5265d5fac07ed"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.1/xissue-v0.8.1-linux-arm64.tar.gz"
      sha256 "5ebaf4f66a136f85d08f8591e8542e5db49b9b7b859cbcaa0b8a7d90fca7eb24"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.1/xissue-v0.8.1-linux-amd64.tar.gz"
      sha256 "990c5529888827e6e1beb58eccff90cf292ce0055fdba5a3e8010b57b2d100c5"
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
