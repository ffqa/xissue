class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.38"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.38/xissue-v0.2.38-darwin-arm64.tar.gz"
      sha256 "d734bae26cb6d15fd692823d94c38b121bc521a09189e7f642d1704b77d43c3a"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.38/xissue-v0.2.38-darwin-amd64.tar.gz"
      sha256 "8789d7f3962b02627d3141020bdcdc7121c2be483fe27d6289b90baa68cb0d00"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.38/xissue-v0.2.38-linux-arm64.tar.gz"
      sha256 "85578813eb9ce1edd203e128347b1abf23bfc27a4173d5bd1e0789758d5e85f7"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.38/xissue-v0.2.38-linux-amd64.tar.gz"
      sha256 "b79140528804b27849a97c1b08d2225923a66a4ca5de8aa46b040f615f884b3c"
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
