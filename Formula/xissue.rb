class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.5.3"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.3/xissue-v0.5.3-darwin-arm64.tar.gz"
      sha256 "644aa0a99ce2dd8f1306acd44600582939a5cecfac284700733319f9b75b89b1"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.3/xissue-v0.5.3-darwin-amd64.tar.gz"
      sha256 "c242bf47f00c31178f2fbb9d9ac08a169d341f19b6a88718ec367cabde3b52e7"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.5.3/xissue-v0.5.3-linux-arm64.tar.gz"
      sha256 "a80ae6167e8e16a292cd0ce76b02d6ca0b1960797b162c9440c4bc7123633887"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.5.3/xissue-v0.5.3-linux-amd64.tar.gz"
      sha256 "cf89d160809f5a9d7219a1ec98d322c5ca57fd6e1eb3e90c52b2d369aed3df36"
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
