#
# Copyright (c) 2024, NVIDIA CORPORATION. All rights reserved.
#

from typing import Optional, Union
from aistore.sdk.request_client import RequestClient
from aistore.sdk.const import (
    HTTP_METHOD_POST,
    URL_PATH_AUTHN_USERS,
)
from aistore.sdk.authn.authn_types import TokenMsg, LoginMsg


# pylint: disable=too-many-arguments, too-few-public-methods
class AuthNClient:
    """
    AuthN client for managing authentication.

    This client provides methods to interact with AuthN Server.
    For more info on AuthN Server, see https://github.com/NVIDIA/aistore/blob/main/docs/authn.md

    Args:
        endpoint (str): AuthN service endpoint URL.
        skip_verify (bool, optional): If True, skip SSL certificate verification. Defaults to False.
        ca_cert (str, optional): Path to a CA certificate file for SSL verification.
        timeout (Union[float, tuple[float, float], None], optional): Request timeout in seconds; a single float
            for both connect/read timeouts (e.g., 5.0), a tuple for separate connect/read timeouts (e.g., (3.0, 10.0)),
            or None to disable timeout.
        token (str, optional): Authorization token.
    """

    def __init__(
        self,
        endpoint: str,
        skip_verify: bool = False,
        ca_cert: Optional[str] = None,
        timeout: Optional[Union[float, tuple[float, float]]] = None,
        token: Optional[str] = None,
    ):
        self._request_client = RequestClient(
            endpoint, skip_verify, ca_cert, timeout, token
        )

    def login(
        self,
        username: str,
        password: str,
        expires_in: Optional[Union[int, float]] = None,
    ) -> str:
        """
        Logs in to the AuthN Server and returns an authorization token.

        Args:
            username (str): The username to log in with.
            password (str): The password to log in with.
            expires_in (Optional[Union[int, float]]): The expiration duration of the token in seconds.

        Returns:
            str: An authorization token to use for future requests.

        Raises:
            ValueError: If the password is empty or consists only of spaces.
            AISError: If the login request fails.
        """
        if password.strip() == "":
            raise ValueError("Password cannot be empty or spaces only")

        login_msg = LoginMsg(password=password, expires_in=expires_in).as_dict()

        return self._request_client.request_deserialize(
            HTTP_METHOD_POST,
            path=f"{URL_PATH_AUTHN_USERS}/{username}",
            json=login_msg,
            res_model=TokenMsg,
        ).token
