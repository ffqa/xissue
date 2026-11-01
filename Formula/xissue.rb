class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.7.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.7.0/xissue-v0.7.0-darwin-arm64.tar.gz"
      sha256 "28dbb4145a3260fe7d1651180fb63d9f4fb67562a9c94cbd2f4accc7c455b6cb"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.7.0/xissue-v0.7.0-darwin-amd64.tar.gz"
      sha256 "71d56a836985ca829e18a03f3595122708603a4fe3e8694e023ce50cc042c383"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.7.0/xissue-v0.7.0-linux-arm64.tar.gz"
      sha256 "296e2f2508aa86b1a9d9c34fc08edf81a8a1617aa531dfa4a559fb57a4cd2f75"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.7.0/xissue-v0.7.0-linux-amd64.tar.gz"
      sha256 "5efc7f2338c6432583ceea0ed51f29b480ce7a0f3797eab9149d71bfc9cd836b"
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
