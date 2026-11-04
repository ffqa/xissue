class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.8.7"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.7/xissue-v0.8.7-darwin-arm64.tar.gz"
      sha256 "8049fa040e608648d8a122b86b7d237493aaad04b52909136d102571f6806c71"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.7/xissue-v0.8.7-darwin-amd64.tar.gz"
      sha256 "e004280cdd17e07f5fb4f905ff84c05ccf688f52f457644bdb31ca7ccd55e402"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.8.7/xissue-v0.8.7-linux-arm64.tar.gz"
      sha256 "766e64fdd8e30e16cd26149441460c85b45b29a265b7c7e89b668c94ff3e3dfc"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.8.7/xissue-v0.8.7-linux-amd64.tar.gz"
      sha256 "94117a6634eab4f1848a843620efc3736791beca538fd35298d843d65ba4a4a9"
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
